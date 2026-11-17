package handler

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// webshellSupportedEncodings the allowed WebShell response encoding values (lowercase, including an empty string for auto)
// only exposes the few most common ones at present; other needs can be extended later (e.g. Big5, Shift_JIS, etc.).
var webshellSupportedEncodings = map[string]struct{}{
	"":        {}, // not configured, handle as auto
	"auto":    {},
	"utf-8":   {},
	"utf8":    {},
	"gbk":     {},
	"gb18030": {},
}

// normalizeWebshellEncoding normalizes the encoding identifier: unified to lowercase, unknown values fall back to auto, for persistence
func normalizeWebshellEncoding(enc string) string {
	enc = strings.ToLower(strings.TrimSpace(enc))
	if _, ok := webshellSupportedEncodings[enc]; !ok {
		return "auto"
	}
	if enc == "" {
		return "auto"
	}
	if enc == "utf8" {
		return "utf-8"
	}
	return enc
}

// decodeWebshellOutput converts the bytes returned by the WebShell into a valid UTF-8 string using the specified encoding.
// convention:
//   - "" / "auto": if already valid UTF-8, return as-is, otherwise try GB18030 (GBK superset) decoding in turn.
//   - "utf-8" / "utf8": return as-is, illegal bytes are handled by the JSON layer as U+FFFD (keeps original behavior).
//   - "gbk" / "gb18030": force decoding with the corresponding encoding; on failure, fall back to the raw bytes.
//
// this function returns an empty string directly for empty input, avoiding an unnecessary conversion.
func decodeWebshellOutput(raw []byte, encoding string) string {
	if len(raw) == 0 {
		return ""
	}
	enc := normalizeWebshellEncoding(encoding)
	switch enc {
	case "utf-8":
		return string(raw)
	case "gbk":
		if out, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), raw); err == nil {
			return string(out)
		}
		return string(raw)
	case "gb18030":
		if out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw); err == nil {
			return string(out)
		}
		return string(raw)
	default: // auto
		if utf8.Valid(raw) {
			return string(raw)
		}
		// GB18030 is a superset of GBK with the widest coverage; auto mode uniformly uses it as a fallback
		if out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw); err == nil {
			return string(out)
		}
		return string(raw)
	}
}

// webshellSupportedOS the allowed WebShell target operating systems (lowercase, empty string for auto)
var webshellSupportedOS = map[string]struct{}{
	"":        {},
	"auto":    {},
	"linux":   {},
	"windows": {},
}

// normalizeWebshellOS normalizes the OS identifier, unknown values fall back to auto, for persistence
func normalizeWebshellOS(osTag string) string {
	osTag = strings.ToLower(strings.TrimSpace(osTag))
	if _, ok := webshellSupportedOS[osTag]; !ok {
		return "auto"
	}
	if osTag == "" {
		return "auto"
	}
	return osTag
}

// resolveWebshellOS infers the final target OS from the connection's os and shellType (returns only "linux" or "windows").
// rules:
//   - explicit linux / windows: by the user's choice.
//   - auto or unknown: asp/aspx → windows, others → linux. Keeps historical behavior for smooth backward compatibility.
func resolveWebshellOS(osTag, shellType string) string {
	osTag = strings.ToLower(strings.TrimSpace(osTag))
	switch osTag {
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	}
	t := strings.ToLower(strings.TrimSpace(shellType))
	if t == "asp" || t == "aspx" {
		return "windows"
	}
	return "linux"
}

// quoteCmdPath escapes a path by Windows cmd.exe rules.
// wrapped in double quotes, internal double quotes escaped as "" (the form cmd accepts).
func quoteCmdPath(p string) string {
	if p == "" {
		return "\".\""
	}
	return "\"" + strings.ReplaceAll(p, "\"", "\"\"") + "\""
}

// normalizeWindowsCmdPath converts the frontend's unified "/" path to "\" which cmd recognizes more reliably.
// only used for Windows command construction, does not change semantics (e.g. "." / ".." stay unchanged).
func normalizeWindowsCmdPath(p string) string {
	s := strings.TrimSpace(p)
	if s == "" {
		return s
	}
	return strings.ReplaceAll(s, "/", "\\")
}

// quotePsSingle escapes a string by PowerShell single-quoted string rules (internal ' → ”).
// used as a PowerShell script argument; the whole script uses only single quotes, and wrapping with cmd double quotes on the outside passes it safely.
func quotePsSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// quoteShellSinglePosix escapes a path by POSIX sh single-quote rules (internal ' → '\”)
func quoteShellSinglePosix(p string) string {
	if p == "" {
		return "."
	}
	return "'" + strings.ReplaceAll(p, "'", "'\\''") + "'"
}

// quoteWebshellPath chooses the escaping scheme by target OS: POSIX single quotes for Linux, cmd double quotes for Windows
func quoteWebshellPath(path, osTag string) string {
	if resolveWebshellOS(osTag, "") == "windows" {
		return quoteCmdPath(path)
	}
	return quoteShellSinglePosix(path)
}

// buildWindowsPowerShellWrite builds the Windows cmd command that writes base64 content to the target path in one shot.
// the outer layer goes through cmd.exe's powershell call; the PowerShell script uses only single-quoted strings to avoid nested-quote traps.
func buildWindowsPowerShellWrite(path, b64 string) string {
	script := "$b=[Convert]::FromBase64String(" + quotePsSingle(b64) + ");" +
		"[IO.File]::WriteAllBytes(" + quotePsSingle(path) + ",$b)"
	return "powershell -NoProfile -NonInteractive -Command \"" + script + "\""
}

// buildWindowsPowerShellAppend builds the Windows cmd command that appends base64 content to the target path (for chunked upload)
func buildWindowsPowerShellAppend(path, b64 string) string {
	script := "$b=[Convert]::FromBase64String(" + quotePsSingle(b64) + ");" +
		"$f=[IO.File]::Open(" + quotePsSingle(path) + ",[IO.FileMode]::Append,[IO.FileAccess]::Write,[IO.FileShare]::None);" +
		"try{$f.Write($b,0,$b.Length)}finally{$f.Close()}"
	return "powershell -NoProfile -NonInteractive -Command \"" + script + "\""
}

// fileCommandInput wraps the input of buildFileCommand to avoid a long parameter list
type fileCommandInput struct {
	Action     string
	Path       string
	TargetPath string
	Content    string
	ChunkIndex int
	OS         string
	ShellType  string
}

// buildFileCommand generates the concrete remote command string based on the target OS and file operation type.
// the same implementation is shared by the HTTP entry (FileOp) and the MCP entry (FileOpWithConnection), avoiding double maintenance.
// the second return value is the user-visible business error (e.g. "path is required").
func (h *WebShellHandler) buildFileCommand(in fileCommandInput) (string, error) {
	targetOS := resolveWebshellOS(in.OS, in.ShellType)
	action := strings.ToLower(strings.TrimSpace(in.Action))
	path := strings.TrimSpace(in.Path)

	switch action {
	case "list":
		p := path
		if p == "" {
			p = "."
		}
		if targetOS == "windows" {
			p = normalizeWindowsCmdPath(p)
			return "dir /a " + quoteCmdPath(p), nil
		}
		return "ls -la " + quoteShellSinglePosix(p), nil

	case "read":
		if path == "" {
			return "", errFileOpPathRequired
		}
		if targetOS == "windows" {
			path = normalizeWindowsCmdPath(path)
			return "type " + quoteCmdPath(path), nil
		}
		return "cat " + quoteShellSinglePosix(path), nil

	case "delete":
		if path == "" {
			return "", errFileOpPathRequired
		}
		if targetOS == "windows" {
			path = normalizeWindowsCmdPath(path)
			return "del /q /f " + quoteCmdPath(path), nil
		}
		return "rm -f " + quoteShellSinglePosix(path), nil

	case "mkdir":
		if path == "" {
			return "", errFileOpPathRequired
		}
		if targetOS == "windows" {
			path = normalizeWindowsCmdPath(path)
			// cmd's md automatically creates intermediate directories by default (equivalent to Linux's mkdir -p)
			return "md " + quoteCmdPath(path), nil
		}
		return "mkdir -p " + quoteShellSinglePosix(path), nil

	case "rename":
		oldPath := path
		newPath := strings.TrimSpace(in.TargetPath)
		if oldPath == "" || newPath == "" {
			return "", errFileOpRenameNeedsBothPaths
		}
		if targetOS == "windows" {
			oldPath = normalizeWindowsCmdPath(oldPath)
			newPath = normalizeWindowsCmdPath(newPath)
			return "move /y " + quoteCmdPath(oldPath) + " " + quoteCmdPath(newPath), nil
		}
		return "mv -f " + quoteShellSinglePosix(oldPath) + " " + quoteShellSinglePosix(newPath), nil

	case "write":
		if path == "" {
			return "", errFileOpPathRequired
		}
		// unified strategy: first base64-encode the content, then decode and write it back using the method corresponding to the target platform,
		// this way it can write arbitrary binary/quoted text while avoiding each shell's escaping hell.
		b64 := base64.StdEncoding.EncodeToString([]byte(in.Content))
		if targetOS == "windows" {
			path = normalizeWindowsCmdPath(path)
			return buildWindowsPowerShellWrite(path, b64), nil
		}
		return "echo '" + b64 + "' | base64 -d > " + quoteShellSinglePosix(path), nil

	case "upload":
		if path == "" {
			return "", errFileOpPathRequired
		}
		if len(in.Content) > 512*1024 {
			return "", errFileOpUploadTooLarge
		}
		if targetOS == "windows" {
			path = normalizeWindowsCmdPath(path)
			return buildWindowsPowerShellWrite(path, in.Content), nil
		}
		return "echo '" + in.Content + "' | base64 -d > " + quoteShellSinglePosix(path), nil

	case "upload_chunk":
		if path == "" {
			return "", errFileOpPathRequired
		}
		if targetOS == "windows" {
			path = normalizeWindowsCmdPath(path)
			if in.ChunkIndex == 0 {
				return buildWindowsPowerShellWrite(path, in.Content), nil
			}
			return buildWindowsPowerShellAppend(path, in.Content), nil
		}
		redir := ">>"
		if in.ChunkIndex == 0 {
			redir = ">"
		}
		return "echo '" + in.Content + "' | base64 -d " + redir + " " + quoteShellSinglePosix(path), nil
	}

	return "", errFileOpUnsupportedAction(action)
}

// business error constants, for upper layers to uniformly return user-visible prompts
var (
	errFileOpPathRequired         = simpleError("path is required")
	errFileOpRenameNeedsBothPaths = simpleError("path and target_path are required for rename")
	errFileOpUploadTooLarge       = simpleError("upload content too large (max 512KB base64)")
)

func errFileOpUnsupportedAction(action string) error {
	return simpleError("unsupported action: " + action)
}

// simpleError is a lightweight error type without a stack, for buildFileCommand to report expected parameter validation errors
type simpleError string

func (e simpleError) Error() string { return string(e) }

// WebShellHandler proxies WebShell command execution (similar to Behinder/AntSword), avoiding frontend cross-origin issues and unifying request construction
type WebShellHandler struct {
	logger *zap.Logger
	client *http.Client
	db     *database.DB
	audit  *audit.Service
}

// SetAudit wires platform audit logging.
func (h *WebShellHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewWebShellHandler creates a WebShell handler, db can be nil (the connection config API will be unavailable)
func NewWebShellHandler(logger *zap.Logger, db *database.DB) *WebShellHandler {
	return &WebShellHandler{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: false,
				// WebShell scenarios commonly use self-signed certificates or IP access (certificate has no IP SAN); verification is skipped by default, consistent with clients such as AntSword.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional for webshell proxy
			},
		},
		db: db,
	}
}

// CreateConnectionRequest create-connection request
type CreateConnectionRequest struct {
	ProjectID string `json:"project_id"`
	URL       string `json:"url" binding:"required"`
	Password  string `json:"password"`
	Type      string `json:"type"`
	Method    string `json:"method"`
	CmdParam  string `json:"cmd_param"`
	Remark    string `json:"remark"`
	Encoding  string `json:"encoding"`
	OS        string `json:"os"`
}

// UpdateConnectionRequest update-connection request
type UpdateConnectionRequest struct {
	ProjectID string `json:"project_id"`
	URL       string `json:"url" binding:"required"`
	Password  string `json:"password"`
	Type      string `json:"type"`
	Method    string `json:"method"`
	CmdParam  string `json:"cmd_param"`
	Remark    string `json:"remark"`
	Encoding  string `json:"encoding"`
	OS        string `json:"os"`
}

// ListConnections lists all WebShell connections (GET /api/webshell/connections)
func (h *WebShellHandler) ListConnections(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	session, _ := security.CurrentSession(c)
	list, err := h.db.ListWebshellConnectionsForAccess(session.UserID, session.Scope, c.Query("project_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []database.WebShellConnection{}
	}
	for i := range list {
		if list[i].Password != "" {
			list[i].Password = maskedSecret
		}
	}
	c.JSON(http.StatusOK, list)
}

// CreateConnection creates a WebShell connection (POST /api/webshell/connections)
func (h *WebShellHandler) CreateConnection(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	var req CreateConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}
	if _, err := url.Parse(req.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if !h.canAccessProject(c, projectID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "project access denied"})
		return
	}
	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method != "get" && method != "post" {
		method = "post"
	}
	shellType := strings.ToLower(strings.TrimSpace(req.Type))
	if shellType == "" {
		shellType = "php"
	}
	conn := &database.WebShellConnection{
		ID:        "ws_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12],
		ProjectID: projectID,
		URL:       req.URL,
		Password:  strings.TrimSpace(req.Password),
		Type:      shellType,
		Method:    method,
		CmdParam:  strings.TrimSpace(req.CmdParam),
		Remark:    strings.TrimSpace(req.Remark),
		Encoding:  normalizeWebshellEncoding(req.Encoding),
		OS:        normalizeWebshellOS(req.OS),
		CreatedAt: time.Now(),
	}
	if err := h.db.CreateWebshellConnection(conn); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if session, ok := security.CurrentSession(c); ok {
		_ = h.db.SetResourceOwner("webshell", conn.ID, session.UserID)
		_ = h.db.AssignResourceToUser(session.UserID, "webshell", conn.ID)
	}
	if h.audit != nil {
		host := req.URL
		if u, err := url.Parse(req.URL); err == nil {
			host = u.Host
		}
		h.audit.RecordOK(c, "webshell", "connection_create", "create WebShell connection", "webshell_connection", conn.ID, map[string]interface{}{
			"host": host, "type": shellType,
		})
	}
	c.JSON(http.StatusOK, publicWebshellConnection(conn))
}

// UpdateConnection updates a WebShell connection (PUT /api/webshell/connections/:id)
func (h *WebShellHandler) UpdateConnection(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	var req UpdateConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}
	if _, err := url.Parse(req.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if !h.canAccessProject(c, projectID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "project access denied"})
		return
	}
	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method != "get" && method != "post" {
		method = "post"
	}
	shellType := strings.ToLower(strings.TrimSpace(req.Type))
	if shellType == "" {
		shellType = "php"
	}
	password := strings.TrimSpace(req.Password)
	if password == maskedSecret {
		stored, ok := h.authorizedWebshellConnection(c, id, "")
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "no permission to access this connection"})
			return
		}
		password = stored.Password
	}
	conn := &database.WebShellConnection{
		ID:        id,
		ProjectID: projectID,
		URL:       req.URL,
		Password:  password,
		Type:      shellType,
		Method:    method,
		CmdParam:  strings.TrimSpace(req.CmdParam),
		Remark:    strings.TrimSpace(req.Remark),
		Encoding:  normalizeWebshellEncoding(req.Encoding),
		OS:        normalizeWebshellOS(req.OS),
	}
	if err := h.db.UpdateWebshellConnection(conn); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	updated, _ := h.db.GetWebshellConnection(id)
	if updated != nil {
		c.JSON(http.StatusOK, publicWebshellConnection(updated))
	} else {
		c.JSON(http.StatusOK, publicWebshellConnection(conn))
	}
}

// DeleteConnection deletes a WebShell connection (DELETE /api/webshell/connections/:id)
func (h *WebShellHandler) DeleteConnection(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	if err := h.db.DeleteWebshellConnection(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete connection"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "webshell", "connection_delete", "delete WebShell connection", "webshell_connection", id, nil)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetConnectionState gets the frontend-persisted state associated with a WebShell connection (GET /api/webshell/connections/:id/state)
func (h *WebShellHandler) GetConnectionState(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	conn, err := h.db.GetWebshellConnection(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if conn == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	stateJSON, err := h.db.GetWebshellConnectionState(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var state interface{}
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		state = map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"state": state})
}

// SaveConnectionState saves the frontend-persisted state associated with a WebShell connection (PUT /api/webshell/connections/:id/state)
func (h *WebShellHandler) SaveConnectionState(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	conn, err := h.db.GetWebshellConnection(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if conn == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	var req struct {
		State json.RawMessage `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	raw := req.State
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > 2*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state payload too large (max 2MB)"})
		return
	}
	var anyJSON interface{}
	if err := json.Unmarshal(raw, &anyJSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state must be valid json"})
		return
	}
	if err := h.db.UpsertWebshellConnectionState(id, string(raw)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetAIHistory gets the AI assistant conversation history of the specified WebShell connection (GET /api/webshell/connections/:id/ai-history)
func (h *WebShellHandler) GetAIHistory(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	conv, err := h.db.GetConversationByWebshellConnectionID(id)
	if err != nil {
		h.logger.Warn("failed to get the WebShell AI conversation", zap.String("connectionId", id), zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"conversationId": nil, "messages": []database.Message{}})
		return
	}
	if conv == nil {
		c.JSON(http.StatusOK, gin.H{"conversationId": nil, "messages": []database.Message{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversationId": conv.ID, "messages": conv.Messages})
}

// ListAIConversations lists all AI conversations under this WebShell connection (for the sidebar)
func (h *WebShellHandler) ListAIConversations(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	list, err := h.db.ListConversationsByWebshellConnectionID(id)
	if err != nil {
		h.logger.Warn("failed to list WebShell AI conversations", zap.String("connectionId", id), zap.Error(err))
		c.JSON(http.StatusOK, []database.WebShellConversationItem{})
		return
	}
	if list == nil {
		list = []database.WebShellConversationItem{}
	}
	c.JSON(http.StatusOK, list)
}

// ExecRequest execute-command request (frontend passes in connection info + command)
type ExecRequest struct {
	URL          string `json:"url" binding:"required"`
	Password     string `json:"password"`
	Type         string `json:"type"`      // php, asp, aspx, jsp, custom
	Method       string `json:"method"`    // GET or POST, defaults to POST when empty
	CmdParam     string `json:"cmd_param"` // the command parameter name, e.g. cmd/xxx, defaults to cmd when empty
	Encoding     string `json:"encoding"`  // response encoding: auto / utf-8 / gbk / gb18030, auto when empty
	OS           string `json:"os"`        // target operating system: auto / linux / windows, currently exec does not use it; the field is kept for future extension
	ConnectionID string `json:"connection_id,omitempty"`
	Command      string `json:"command" binding:"required"`
}

// ExecResponse execute-command response
type ExecResponse struct {
	OK       bool   `json:"ok"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	HTTPCode int    `json:"http_code,omitempty"`
}

// FileOpRequest file-operation request
type FileOpRequest struct {
	URL          string `json:"url" binding:"required"`
	Password     string `json:"password"`
	Type         string `json:"type"`
	Method       string `json:"method"`                    // GET or POST, defaults to POST when empty
	CmdParam     string `json:"cmd_param"`                 // the command parameter name, e.g. cmd/xxx, defaults to cmd when empty
	Encoding     string `json:"encoding"`                  // response encoding: auto / utf-8 / gbk / gb18030, auto when empty
	OS           string `json:"os"`                        // target operating system: auto / linux / windows, inferred from shellType when empty
	ConnectionID string `json:"connection_id,omitempty"`   // optional: connection ID; after the server probes the OS it is written back to this connection
	Action       string `json:"action" binding:"required"` // list, read, delete, write, mkdir, rename, upload, upload_chunk
	Path         string `json:"path"`
	TargetPath   string `json:"target_path"` // the target path for rename
	Content      string `json:"content"`     // used for write/upload
	ChunkIndex   int    `json:"chunk_index"` // for upload_chunk, 0 means the first chunk
}

// FileOpResponse file-operation response
type FileOpResponse struct {
	OK         bool   `json:"ok"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	DetectedOS string `json:"detected_os,omitempty"` // returned only in auto mode when the probe succeeds; the frontend should update its local cache
}

func (h *WebShellHandler) Exec(c *gin.Context) {
	var req ExecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.Command = strings.TrimSpace(req.Command)
	if req.URL == "" || req.Command == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url and command are required"})
		return
	}
	// Pre-save connectivity tests send form credentials without connection_id.
	// Saved connections must go through resource ACL; DB credentials are authoritative.
	if cid := strings.TrimSpace(req.ConnectionID); cid != "" {
		conn, allowed := h.authorizedWebshellConnection(c, cid, req.URL)
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "no permission to access this resource"})
			return
		}
		// Never let a caller pair an authorized ID with attacker-controlled
		// transport credentials or a URL.
		req.URL, req.Password, req.Type = conn.URL, conn.Password, conn.Type
		req.Method, req.CmdParam, req.Encoding = conn.Method, conn.CmdParam, conn.Encoding
	} else if !security.SessionHasPermission(c, "webshell:write") {
		c.JSON(http.StatusForbidden, gin.H{"error": "no permission to access this resource"})
		return
	}
	if req.Password == maskedSecret {
		c.JSON(http.StatusBadRequest, gin.H{"error": "please use a saved connection or fill in a new connection password"})
		return
	}

	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url: only http(s) allowed"})
		return
	}

	useGET := strings.ToUpper(strings.TrimSpace(req.Method)) == "GET"
	cmdParam := strings.TrimSpace(req.CmdParam)
	if cmdParam == "" {
		cmdParam = "cmd"
	}
	var httpReq *http.Request
	if useGET {
		targetURL := h.buildExecURL(req.URL, req.Type, req.Password, cmdParam, req.Command)
		httpReq, err = http.NewRequest(http.MethodGet, targetURL, nil)
	} else {
		body := h.buildExecBody(req.Type, req.Password, cmdParam, req.Command)
		httpReq, err = http.NewRequest(http.MethodPost, req.URL, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		h.logger.Warn("webshell exec NewRequest", zap.Error(err))
		c.JSON(http.StatusInternalServerError, ExecResponse{OK: false, Error: err.Error()})
		return
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HodaEngineAI-WebShell/1.0)")

	resp, err := h.client.Do(httpReq)
	if err != nil {
		h.logger.Warn("webshell exec Do", zap.String("url", req.URL), zap.Error(err))
		c.JSON(http.StatusOK, ExecResponse{OK: false, Error: err.Error()})
		return
	}
	defer resp.Body.Close()

	out, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		h.logger.Warn("webshell exec read body", zap.Error(readErr))
	}
	output := decodeWebshellOutput(out, req.Encoding)
	httpCode := resp.StatusCode

	ok := resp.StatusCode == http.StatusOK
	c.JSON(http.StatusOK, ExecResponse{
		OK:       ok,
		Output:   output,
		HTTPCode: httpCode,
	})
}

// buildExecBody builds the POST body by common WebShell conventions (most use pass + cmd, the command parameter name is configurable)
func (h *WebShellHandler) buildExecBody(shellType, password, cmdParam, command string) []byte {
	form := h.execParams(shellType, password, cmdParam, command)
	return []byte(form.Encode())
}

// buildExecURL builds the full URL for a GET request (baseURL + ?pass=xxx&cmd=yyy, cmd is configurable)
func (h *WebShellHandler) buildExecURL(baseURL, shellType, password, cmdParam, command string) string {
	form := h.execParams(shellType, password, cmdParam, command)
	if parsed, err := url.Parse(baseURL); err == nil {
		parsed.RawQuery = form.Encode()
		return parsed.String()
	}
	return baseURL + "?" + form.Encode()
}

func (h *WebShellHandler) execParams(shellType, password, cmdParam, command string) url.Values {
	shellType = strings.ToLower(strings.TrimSpace(shellType))
	if shellType == "" {
		shellType = "php"
	}
	if strings.TrimSpace(cmdParam) == "" {
		cmdParam = "cmd"
	}
	form := url.Values{}
	form.Set("pass", password)
	form.Set(cmdParam, command)
	return form
}

func (h *WebShellHandler) FileOp(c *gin.Context) {
	var req FileOpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.URL == "" || req.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url and action are required"})
		return
	}
	if cid := strings.TrimSpace(req.ConnectionID); cid != "" {
		conn, allowed := h.authorizedWebshellConnection(c, cid, req.URL)
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "no permission to access this resource"})
			return
		}
		req.URL, req.Password, req.Type = conn.URL, conn.Password, conn.Type
		req.Method, req.CmdParam, req.Encoding, req.OS = conn.Method, conn.CmdParam, conn.Encoding, conn.OS
	} else if !security.SessionHasPermission(c, "webshell:write") {
		c.JSON(http.StatusForbidden, gin.H{"error": "no permission to access this resource"})
		return
	}

	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url: only http(s) allowed"})
		return
	}

	// if the OS is not explicitly configured, first send a probe command, identify the real OS, then build the file-operation command.
	// this solves the problem where, in the "Windows + PHP + OS=auto" scenario, the old fallback wrongly sent `ls -la` and could not list the directory.
	osTag := req.OS
	detectedOS := ""
	if normalizeWebshellOS(osTag) == "auto" {
		if probed := probeWebshellOSViaExec(h.newHTTPExecFn(req.URL, req.Password, req.Type, req.Method, req.CmdParam, req.Encoding)); probed != "" {
			osTag = probed
			detectedOS = probed
			// if the frontend passed a connection_id, also persist the probe result to that connection, so subsequent refreshes have zero cost
			if cid := strings.TrimSpace(req.ConnectionID); cid != "" {
				h.persistDetectedOS(cid, probed)
			}
		}
	}

	command, cmdErr := h.buildFileCommand(fileCommandInput{
		Action:     req.Action,
		Path:       req.Path,
		TargetPath: req.TargetPath,
		Content:    req.Content,
		ChunkIndex: req.ChunkIndex,
		OS:         osTag,
		ShellType:  req.Type,
	})
	if cmdErr != nil {
		c.JSON(http.StatusBadRequest, FileOpResponse{OK: false, Error: cmdErr.Error()})
		return
	}

	useGET := strings.ToUpper(strings.TrimSpace(req.Method)) == "GET"
	cmdParam := strings.TrimSpace(req.CmdParam)
	if cmdParam == "" {
		cmdParam = "cmd"
	}
	var httpReq *http.Request
	if useGET {
		targetURL := h.buildExecURL(req.URL, req.Type, req.Password, cmdParam, command)
		httpReq, err = http.NewRequest(http.MethodGet, targetURL, nil)
	} else {
		body := h.buildExecBody(req.Type, req.Password, cmdParam, command)
		httpReq, err = http.NewRequest(http.MethodPost, req.URL, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, FileOpResponse{OK: false, Error: err.Error()})
		return
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HodaEngineAI-WebShell/1.0)")

	resp, err := h.client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusOK, FileOpResponse{OK: false, Error: err.Error()})
		return
	}
	defer resp.Body.Close()

	out, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		h.logger.Warn("webshell fileop read body", zap.Error(readErr))
	}
	output := decodeWebshellOutput(out, req.Encoding)

	c.JSON(http.StatusOK, FileOpResponse{
		OK:         resp.StatusCode == http.StatusOK,
		Output:     output,
		DetectedOS: detectedOS,
	})
}

func (h *WebShellHandler) authorizedWebshellConnection(c *gin.Context, connectionID, requestURL string) (*database.WebShellConnection, bool) {
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" {
		return nil, false
	}
	if h.db == nil {
		return nil, false
	}
	session, ok := security.CurrentSession(c)
	if !ok || !h.db.UserCanAccessResource(session.UserID, session.Scope, "webshell", connectionID) {
		return nil, false
	}
	conn, err := h.db.GetWebshellConnection(connectionID)
	if err != nil || conn == nil {
		return nil, false
	}
	if requestURL = strings.TrimSpace(requestURL); requestURL != "" && strings.TrimSpace(conn.URL) != requestURL {
		return nil, false
	}
	return conn, true
}

func (h *WebShellHandler) canAccessProject(c *gin.Context, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || h.db == nil {
		return true
	}
	session, ok := security.CurrentSession(c)
	if !ok {
		return false
	}
	if session.Scope == database.RBACScopeAll {
		return true
	}
	return h.db.UserCanAccessResource(session.UserID, session.Scope, "project", projectID)
}

// ExecWithConnection executes a command on the specified WebShell connection (for non-HTTP callers such as MCP/Agent)
func (h *WebShellHandler) ExecWithConnection(conn *database.WebShellConnection, command string) (output string, ok bool, errMsg string) {
	if conn == nil {
		return "", false, "connection is nil"
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false, "command is required"
	}
	useGET := strings.ToUpper(strings.TrimSpace(conn.Method)) == "GET"
	cmdParam := strings.TrimSpace(conn.CmdParam)
	if cmdParam == "" {
		cmdParam = "cmd"
	}
	var httpReq *http.Request
	var err error
	if useGET {
		targetURL := h.buildExecURL(conn.URL, conn.Type, conn.Password, cmdParam, command)
		httpReq, err = http.NewRequest(http.MethodGet, targetURL, nil)
	} else {
		body := h.buildExecBody(conn.Type, conn.Password, cmdParam, command)
		httpReq, err = http.NewRequest(http.MethodPost, conn.URL, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		return "", false, err.Error()
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HodaEngineAI-WebShell/1.0)")
	resp, err := h.client.Do(httpReq)
	if err != nil {
		return "", false, err.Error()
	}
	defer resp.Body.Close()
	out, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		h.logger.Warn("webshell ExecWithConnection read body", zap.Error(readErr))
	}
	return decodeWebshellOutput(out, conn.Encoding), resp.StatusCode == http.StatusOK, ""
}

// FileOpWithConnection performs a file operation on the specified WebShell connection (for MCP/Agent callers), supports list / read / write
func (h *WebShellHandler) FileOpWithConnection(conn *database.WebShellConnection, action, path, content, targetPath string) (output string, ok bool, errMsg string) {
	if conn == nil {
		return "", false, "connection is nil"
	}
	action = strings.ToLower(strings.TrimSpace(action))
	// MCP entry only exposes the three actions list / read / write, consistent with the promise in the tool documentation
	switch action {
	case "list", "read", "write":
		// supported actions
	default:
		return "", false, "unsupported action: " + action + " (supported: list, read, write)"
	}

	// if the connection's OS is auto, probe and persist first, to avoid the AI/MCP sending `ls -la` to Windows every time
	osTag := conn.OS
	if normalizeWebshellOS(osTag) == "auto" {
		if probed := probeWebshellOSViaExec(func(cmd string) (string, bool) {
			out, exOk, _ := h.ExecWithConnection(conn, cmd)
			return out, exOk
		}); probed != "" {
			osTag = probed
			conn.OS = probed // use the probe result within this request
			h.persistDetectedOS(conn.ID, probed)
		}
	}

	command, cmdErr := h.buildFileCommand(fileCommandInput{
		Action:     action,
		Path:       path,
		TargetPath: targetPath,
		Content:    content,
		OS:         osTag,
		ShellType:  conn.Type,
	})
	if cmdErr != nil {
		return "", false, cmdErr.Error()
	}
	useGET := strings.ToUpper(strings.TrimSpace(conn.Method)) == "GET"
	cmdParam := strings.TrimSpace(conn.CmdParam)
	if cmdParam == "" {
		cmdParam = "cmd"
	}
	var httpReq *http.Request
	var err error
	if useGET {
		targetURL := h.buildExecURL(conn.URL, conn.Type, conn.Password, cmdParam, command)
		httpReq, err = http.NewRequest(http.MethodGet, targetURL, nil)
	} else {
		body := h.buildExecBody(conn.Type, conn.Password, cmdParam, command)
		httpReq, err = http.NewRequest(http.MethodPost, conn.URL, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		return "", false, err.Error()
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HodaEngineAI-WebShell/1.0)")
	resp, err := h.client.Do(httpReq)
	if err != nil {
		return "", false, err.Error()
	}
	defer resp.Body.Close()
	out, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		h.logger.Warn("webshell FileOpWithConnection read body", zap.Error(readErr))
	}
	return decodeWebshellOutput(out, conn.Encoding), resp.StatusCode == http.StatusOK, ""
}
