// Package c2 implements CyberStrikeAI's built-in C2 (Command & Control) framework.
//
// Design overview:
//   - Manager is the unified entry point, instantiated by internal/app and injected into all components that need to control C2
//     (HTTP handler, MCP tools, HITL bridge, attack-chain recorder, etc.).
//   - Listener is an abstract interface with concrete implementations for tcp_reverse / http_beacon / https_beacon / websocket
//     and other transports, all created via the listener.Registry factory.
//   - Task scheduling uses a hybrid of the database (the c2_tasks table) + an in-memory event bus (EventBus):
//     * state changes and history are persisted and restored across restarts via SQLite;
//     * high-frequency real-time notifications (such as new task results) are pushed to SSE/WS subscribers via the EventBus, avoiding polling.
//   - The crypto layer is fixed at AES-256-GCM, with each Listener having an independent 32-byte key; the key is held only by the server
//     and injected into the implant at compile time; the event stream must not export the plaintext key.
package c2

import (
	"errors"
	"strings"
	"time"
)

// ListenerType is the listener type, matching the c2_listeners.type field
type ListenerType string

const (
	ListenerTypeTCPReverse   ListenerType = "tcp_reverse"
	ListenerTypeHTTPBeacon   ListenerType = "http_beacon"
	ListenerTypeHTTPSBeacon  ListenerType = "https_beacon"
	ListenerTypeWebSocket    ListenerType = "websocket"
)

// AllListenerTypes lists all supported listener types, for validation and frontend enumeration
func AllListenerTypes() []ListenerType {
	return []ListenerType{
		ListenerTypeTCPReverse,
		ListenerTypeHTTPBeacon,
		ListenerTypeHTTPSBeacon,
		ListenerTypeWebSocket,
	}
}

// IsValidListenerType validates whether a frontend/MCP input is a valid type
func IsValidListenerType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, lt := range AllListenerTypes() {
		if string(lt) == t {
			return true
		}
	}
	return false
}

// SessionStatus matches c2_sessions.status
type SessionStatus string

const (
	SessionActive   SessionStatus = "active"
	SessionSleeping SessionStatus = "sleeping"
	SessionDead     SessionStatus = "dead"
	SessionKilled   SessionStatus = "killed"
)

// TaskStatus matches c2_tasks.status
type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskSent      TaskStatus = "sent"
	TaskRunning   TaskStatus = "running"
	TaskSuccess   TaskStatus = "success"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// TaskType is the task type (negotiated with the beacon side, avoiding hard-coded strings)
type TaskType string

const (
	// Common tasks
	TaskTypeExec       TaskType = "exec"        // execute an arbitrary command (shell -c)
	TaskTypeShell      TaskType = "shell"       // interactive command (preserving cwd)
	TaskTypePwd        TaskType = "pwd"         // current directory
	TaskTypeCd         TaskType = "cd"          // change directory
	TaskTypeLs         TaskType = "ls"          // list directory
	TaskTypePs         TaskType = "ps"          // list processes
	TaskTypeKillProc   TaskType = "kill_proc"   // kill a process
	TaskTypeUpload     TaskType = "upload"      // push a file to the target
	TaskTypeDownload   TaskType = "download"    // pull a file back to the local machine
	TaskTypeScreenshot TaskType = "screenshot"  // screenshot
	TaskTypeSleep      TaskType = "sleep"       // adjust the heartbeat cadence
	TaskTypeExit       TaskType = "exit"        // make the implant exit (does not self-delete the binary)
	TaskTypeSelfDelete TaskType = "self_delete" // exit + self-delete the binary (persistence cleanup)
	// Advanced tasks
	TaskTypePortFwd      TaskType = "port_fwd"
	TaskTypeSocksStart   TaskType = "socks_start"
	TaskTypeSocksStop    TaskType = "socks_stop"
	TaskTypeLoadAssembly TaskType = "load_assembly"
	TaskTypePersist      TaskType = "persist"
)

// AllTaskTypes are all task_types, for a tool schema to list the enum
func AllTaskTypes() []TaskType {
	return []TaskType{
		TaskTypeExec, TaskTypeShell,
		TaskTypePwd, TaskTypeCd, TaskTypeLs, TaskTypePs, TaskTypeKillProc,
		TaskTypeUpload, TaskTypeDownload, TaskTypeScreenshot,
		TaskTypeSleep, TaskTypeExit, TaskTypeSelfDelete,
		TaskTypePortFwd, TaskTypeSocksStart, TaskTypeSocksStop, TaskTypeLoadAssembly,
		TaskTypePersist,
	}
}

// IsDangerousTaskType marks task types that require HITL secondary confirmation;
// it echoes the existing tool_whitelist concept in internal/handler/hitl.go: outside the whitelist -> go through approval.
func IsDangerousTaskType(t TaskType) bool {
	switch t {
	case TaskTypeKillProc, TaskTypeUpload, TaskTypeSelfDelete,
		TaskTypePortFwd, TaskTypeSocksStart, TaskTypeLoadAssembly, TaskTypePersist:
		return true
	}
	return false
}

// ListenerConfig is the decoded listener runtime configuration (from c2_listeners.config_json)
type ListenerConfig struct {
	// Common fields for HTTP/HTTPS Beacon
	BeaconCheckInPath string `json:"beacon_check_in_path,omitempty"` // default "/check_in"
	BeaconTasksPath   string `json:"beacon_tasks_path,omitempty"`    // default "/tasks"
	BeaconResultPath  string `json:"beacon_result_path,omitempty"`   // default "/result"
	BeaconUploadPath  string `json:"beacon_upload_path,omitempty"`   // default "/upload"
	BeaconFilePath    string `json:"beacon_file_path,omitempty"`     // default "/file/"
	// HTTPS-specific
	TLSCertPath string `json:"tls_cert_path,omitempty"`
	TLSKeyPath  string `json:"tls_key_path,omitempty"`
	TLSAutoSelfSign bool `json:"tls_auto_self_sign,omitempty"` // true: auto-generate a self-signed certificate when none is found
	// Client default parameters (written as the initial values in c2_sessions; the beacon may also override them at check-in)
	DefaultSleep  int `json:"default_sleep,omitempty"`  // seconds, default 5
	DefaultJitter int `json:"default_jitter,omitempty"` // 0-100, default 0
	// OPSEC: optional command blacklist (regex)
	CommandDenyRegex []string `json:"command_deny_regex,omitempty"`
	// Task concurrency limit (the maximum number of tasks dispatched simultaneously per session; 0 means no limit)
	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`
	// CallbackHost is the callback hostname used by the implant/payload (optional); separate from bind_host, for NAT/ECS and similar scenarios
	CallbackHost string `json:"callback_host,omitempty"`
	// When AllowLegacyShell is true, tcp_reverse allows an unencrypted classic bash/nc reverse shell to register a session (default false; strongly discouraged for public-internet deployments)
	AllowLegacyShell bool `json:"allow_legacy_shell,omitempty"`
}

// ApplyDefaults fills unset fields with default values; the caller is responsible for serializing the new values when persisting
func (c *ListenerConfig) ApplyDefaults() {
	if strings.TrimSpace(c.BeaconCheckInPath) == "" {
		c.BeaconCheckInPath = "/check_in"
	}
	if strings.TrimSpace(c.BeaconTasksPath) == "" {
		c.BeaconTasksPath = "/tasks"
	}
	if strings.TrimSpace(c.BeaconResultPath) == "" {
		c.BeaconResultPath = "/result"
	}
	if strings.TrimSpace(c.BeaconUploadPath) == "" {
		c.BeaconUploadPath = "/upload"
	}
	if strings.TrimSpace(c.BeaconFilePath) == "" {
		c.BeaconFilePath = "/file/"
	}
	if c.DefaultSleep <= 0 {
		c.DefaultSleep = 5
	}
	if c.DefaultJitter < 0 {
		c.DefaultJitter = 0
	}
	if c.DefaultJitter > 100 {
		c.DefaultJitter = 100
	}
}

// ImplantCheckInRequest is the registration/heartbeat request body from beacon -> server (the decrypted plaintext)
type ImplantCheckInRequest struct {
	ImplantUUID  string                 `json:"uuid"`
	Hostname     string                 `json:"hostname"`
	Username     string                 `json:"username"`
	OS           string                 `json:"os"`
	Arch         string                 `json:"arch"`
	PID          int                    `json:"pid"`
	ProcessName  string                 `json:"process_name"`
	IsAdmin      bool                   `json:"is_admin"`
	InternalIP   string                 `json:"internal_ip"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	SleepSeconds int                    `json:"sleep_seconds"`
	JitterPercent int                   `json:"jitter_percent"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ImplantCheckInResponse is the server's receipt
type ImplantCheckInResponse struct {
	SessionID    string `json:"session_id"`
	NextSleep    int    `json:"next_sleep"`
	NextJitter   int    `json:"next_jitter"`
	HasTasks     bool   `json:"has_tasks"`
	ServerTime   int64  `json:"server_time"`
}

// TaskEnvelope is the task-dispatch carrier from server -> beacon
type TaskEnvelope struct {
	TaskID   string                 `json:"task_id"`
	TaskType string                 `json:"task_type"`
	Payload  map[string]interface{} `json:"payload"`
}

// TaskResultReport is the task-result report from beacon -> server
type TaskResultReport struct {
	TaskID     string `json:"task_id"`
	Success    bool   `json:"success"`
	Output     string `json:"output,omitempty"`
	OutputB64  string `json:"output_b64,omitempty"` // raw console bytes (base64), to avoid JSON corrupting non-UTF-8 output
	Error      string `json:"error,omitempty"`
	ErrorB64   string `json:"error_b64,omitempty"`
	BlobBase64 string `json:"blob_b64,omitempty"` // e.g. screenshot binary
	BlobSuffix string `json:"blob_suffix,omitempty"` // e.g. ".png"
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at"`
}

// CommonError is the C2 module's unified error type, making it easy for the handler layer to map HTTP status codes
type CommonError struct {
	Code    string
	Message string
	HTTP    int
}

func (e *CommonError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Sentinel errors, for errors.Is comparison
var (
	ErrListenerNotFound = &CommonError{Code: "listener_not_found", Message: "listener does not exist", HTTP: 404}
	ErrSessionNotFound  = &CommonError{Code: "session_not_found", Message: "session does not exist", HTTP: 404}
	ErrTaskNotFound     = &CommonError{Code: "task_not_found", Message: "task does not exist", HTTP: 404}
	ErrProfileNotFound  = &CommonError{Code: "profile_not_found", Message: "Profile does not exist", HTTP: 404}
	ErrInvalidInput     = &CommonError{Code: "invalid_input", Message: "invalid parameters", HTTP: 400}
	ErrAuthFailed       = &CommonError{Code: "auth_failed", Message: "authentication failed", HTTP: 401}
	ErrPortInUse        = &CommonError{Code: "port_in_use", Message: "port is already in use", HTTP: 409}
	ErrListenerRunning  = &CommonError{Code: "listener_running", Message: "listener is already running", HTTP: 409}
	ErrListenerStopped  = &CommonError{Code: "listener_stopped", Message: "listener is not running", HTTP: 409}
	ErrUnsupportedType  = &CommonError{Code: "unsupported_type", Message: "unsupported listener type", HTTP: 400}
)

// SafeBindPort validates the port range
func SafeBindPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("port must be in 1..65535")
	}
	return nil
}

// NowUnixMillis is a unified timestamp utility
func NowUnixMillis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
