package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// webshellOSProbeCommand the probe command: determines the OS by the difference in how Windows cmd and POSIX shell expand `%OS%`.
//   - Windows cmd: `%OS%` is expanded to `Windows_NT`, echoes back `:OSPROBE_Windows_NT:END`
//   - POSIX sh/bash: `%OS%` is not variable syntax, kept as a literal, echoes back `:OSPROBE_%OS%:END`
//
// one command yields a clear, mutually exclusive signal, avoiding the probe cost (compared to sending two commands).
// the colon wrapping is to avoid the string match failing when some shells output extra whitespace/BOM.
const webshellOSProbeCommand = "echo :OSPROBE_%OS%:END"

// probeWebshellOSViaExec infers the target operating system from the echo of one command execution.
//
// return values:
//   - "windows" / "linux": recognized successfully
//   - "": cannot be determined (the caller should keep its existing fallback logic)
//
// the input execFn is a "send command and get echo" closure; lets the HTTP entry and MCP entry share the same probe logic
// without having to care about how the packets are sent underneath.
func probeWebshellOSViaExec(execFn func(cmd string) (output string, ok bool)) string {
	if execFn == nil {
		return ""
	}
	out, ok := execFn(webshellOSProbeCommand)
	if !ok {
		return ""
	}
	return classifyWebshellOSProbeOutput(out)
}

// classifyWebshellOSProbeOutput pure function: determines the OS from the probe command's echo.
// extracted so unit tests can directly cover all branches without a real HTTP call.
func classifyWebshellOSProbeOutput(out string) string {
	if out == "" {
		return ""
	}
	lower := strings.ToLower(out)

	// Windows strong signal: cmd.exe successfully expanded the %OS% variable
	if strings.Contains(out, "Windows_NT") {
		return "windows"
	}
	// fault tolerance: some old versions of Windows may expand `%OS%` to other text (very rare), then look at secondary clues such as PATH/OS
	if strings.Contains(lower, "microsoft windows") {
		return "windows"
	}

	// Linux/Unix strong signal: the `%OS%` literal is echoed back as-is, indicating the shell is not cmd.exe
	if strings.Contains(out, "%OS%") {
		return "linux"
	}

	// secondary clue: some webshells on Linux may use another shell (e.g. zsh/ash),
	// but they also do not expand `%OS%`; if the OSPROBE header is matched but no %OS% literal is obtained,
	// indicates the echo was truncated or filtered midway; conservatively return empty and let the upper layer fall back.
	return ""
}

// newHTTPExecFn builds the "send command and get echo" closure for the HTTP FileOp path, reused by the liveness probe.
// the parameters come from the HTTP request, reusing the two existing command orchestrators buildExecURL / buildExecBody,
// ensure the probe packet and the actual file-operation packet use exactly the same webshell protocol (GET/POST, parameter names, encoding).
func (h *WebShellHandler) newHTTPExecFn(targetURL, password, shellType, method, cmdParam, encoding string) func(string) (string, bool) {
	useGET := strings.ToUpper(strings.TrimSpace(method)) == "GET"
	if strings.TrimSpace(cmdParam) == "" {
		cmdParam = "cmd"
	}
	return func(cmd string) (string, bool) {
		var (
			httpReq *http.Request
			err     error
		)
		if useGET {
			u := h.buildExecURL(targetURL, shellType, password, cmdParam, cmd)
			httpReq, err = http.NewRequest(http.MethodGet, u, nil)
		} else {
			body := h.buildExecBody(shellType, password, cmdParam, cmd)
			httpReq, err = http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
			if err == nil {
				httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
		}
		if err != nil {
			return "", false
		}
		httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Hoda Engine-WebShell/1.0)")
		resp, err := h.client.Do(httpReq)
		if err != nil {
			return "", false
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return decodeWebshellOutput(raw, encoding), resp.StatusCode == http.StatusOK
	}
}

// persistDetectedOS writes the probe result back to the connection table; a failure only logs and does not block the main flow.
// by design, intentionally only triggers an UPDATE, never creates a record, so even if connectionID does not exist it just silently gives up.
func (h *WebShellHandler) persistDetectedOS(connectionID, detected string) {
	connectionID = strings.TrimSpace(connectionID)
	detected = normalizeWebshellOS(detected)
	if connectionID == "" || detected == "" || detected == "auto" {
		return
	}
	conn, err := h.db.GetWebshellConnection(connectionID)
	if err != nil || conn == nil {
		// not all callers can provide a valid ID (e.g. temporary tests), so return silently here
		return
	}
	if normalizeWebshellOS(conn.OS) != "auto" {
		// the user has already explicitly chosen an OS; respect the user's choice, do not auto-override
		return
	}
	conn.OS = detected
	if err := h.db.UpdateWebshellConnection(conn); err != nil {
		h.logger.Warn("failed to persist the webshell probe result", zap.String("id", connectionID), zap.String("os", detected), zap.Error(err))
		return
	}
	h.logger.Info("webshell auto OS probe succeeded and persisted", zap.String("id", connectionID), zap.String("os", detected))
}
