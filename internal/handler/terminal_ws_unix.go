//go:build !windows

package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// terminalResize is sent by the frontend when the xterm.js terminal is resized.
type terminalResize struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// wsUpgrader used only for the terminal WebSocket in system settings; reuses the existing login protection (the JWT middleware is on the parent route group)
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// since authentication is already done at the Gin routing layer, Origin is relaxed here to allow access over HTTPS/WSS under the same domain
		return true
	},
}

// RunCommandWS provides a truly interactive shell: a long-lived session based on WebSocket + PTY
// after the frontend establishes a WebSocket connection, all keyboard input is passed through to the shell, and the shell's output is written back to the frontend in real time.
func (h *TerminalHandler) RunCommandWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// start an interactive shell, preferring bash and falling back to sh if not found
	shell := "bash"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "sh"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"COLUMNS=80",
		"LINES=24",
		"TERM=xterm-256color",
	)

	// Use 80x24 as a safe default; the frontend will send the actual size immediately after connecting.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		return
	}
	defer ptmx.Close()

	// Shell -> WebSocket: send PTY output to the frontend in real time
	doneChan := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				_ = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if err != nil {
				break
			}
		}
		close(doneChan)
	}()

	// WebSocket -> Shell: write frontend input to the PTY (including the sudo password, Ctrl+C, etc.)
	conn.SetReadLimit(64 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(terminalTimeout))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(terminalTimeout))
		return nil
	})

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			_ = cmd.Process.Kill()
			break
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}
		if len(data) == 0 {
			continue
		}
		// Check if this is a resize message (JSON with type:"resize")
		if msgType == websocket.TextMessage && len(data) > 0 && data[0] == '{' {
			var resize terminalResize
			if json.Unmarshal(data, &resize) == nil && resize.Type == "resize" && resize.Cols > 0 && resize.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: resize.Cols, Rows: resize.Rows})
				continue
			}
		}
		if _, err := ptmx.Write(data); err != nil {
			_ = cmd.Process.Kill()
			break
		}
	}

	<-doneChan
}
