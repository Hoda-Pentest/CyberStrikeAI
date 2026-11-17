package handler

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	robotCmdHelp          = "\u5e2e\u52a9"             // Chinese command alias: help
	robotCmdList          = "\u5217\u8868"             // Chinese command alias: list
	robotCmdListAlt       = "\u5bf9\u8bdd\u5217\u8868" // Chinese command alias: conversation list
	robotCmdSwitch        = "\u5207\u6362"             // Chinese command alias: switch
	robotCmdContinue      = "\u7ee7\u7eed"             // Chinese command alias: continue
	robotCmdNew           = "\u65b0\u5bf9\u8bdd"       // Chinese command alias: new conversation
	robotCmdClear         = "\u6e05\u7a7a"             // Chinese command alias: clear
	robotCmdStatus        = "\u72b6\u6001"             // Chinese command alias: status
	robotCmdStop          = "\u505c\u6b62"             // Chinese command alias: stop
	robotCmdRoles         = "\u89d2\u8272"             // Chinese command alias: role
	robotCmdRolesList     = "\u89d2\u8272\u5217\u8868" // Chinese command alias: role list
	robotCmdSwitchRole    = "\u5207\u6362\u89d2\u8272" // Chinese command alias: switch role
	robotCmdModes         = "\u6a21\u5f0f"             // Chinese command alias: mode
	robotCmdModesList     = "\u6a21\u5f0f\u5217\u8868" // Chinese command alias: mode list
	robotCmdSwitchMode    = "\u5207\u6362\u6a21\u5f0f" // Chinese command alias: switch mode
	robotCmdDelete        = "\u5220\u9664"             // Chinese command alias: delete
	robotCmdVersion       = "\u7248\u672c"             // Chinese command alias: version
	robotCmdProjects      = "\u9879\u76ee"             // Chinese command alias: project
	robotCmdProjectsList  = "\u9879\u76ee\u5217\u8868" // Chinese command alias: project list
	robotCmdBindProject   = "\u7ed1\u5b9a\u9879\u76ee" // Chinese command alias: bind project
	robotCmdNewProject    = "\u65b0\u5efa\u9879\u76ee" // Chinese command alias: new project
	robotCmdUnbindProject = "\u89e3\u9664\u9879\u76ee" // Chinese command alias: unbind project
	robotCmdBindUser      = "\u7ed1\u5b9a"             // Chinese command alias: bind
	robotCmdUnbindUser    = "\u89e3\u7ed1"             // Chinese command alias: unbind
	robotCmdIdentity      = "\u8eab\u4efd"             // Chinese command alias: identity
	robotCmdTask          = "\u4efb\u52a1"             // Chinese command alias: task
	robotCmdRename        = "\u91cd\u547d\u540d"       // Chinese command alias: rename
	robotCmdPermissions   = "\u6743\u9650"             // Chinese command alias: permissions
	robotCmdDoctor        = "\u8bca\u65ad"             // Chinese command alias: diagnostics
	robotCmdConfirm       = "\u786e\u8ba4"             // Chinese command alias: confirm
	robotCmdCancel        = "\u53d6\u6d88"             // Chinese command alias: cancel
	robotCmdVulnAlerts    = "\u6f0f\u6d1e\u63d0\u9192" // Chinese command alias: vulnerability alerts
	robotBindingCodeTTL   = 5 * time.Minute
)

type robotPendingConfirmation struct {
	Action    string
	Target    string
	ExpiresAt time.Time
}

// RobotHandler handles callbacks from bots such as WeCom/DingTalk/Feishu
type RobotHandler struct {
	config               *config.Config
	db                   *database.DB
	agentHandler         *AgentHandler
	logger               *zap.Logger
	mu                   sync.RWMutex
	sessions             map[string]string             // key: "platform_userID", value: conversationID
	sessionRoles         map[string]string             // key: "platform_userID", value: roleName (defaults to the default role)
	sessionModes         map[string]string             // key: "platform_userID", value: agent mode
	cancelMu             sync.Mutex                    // protects runningCancels
	runningCancels       map[string]context.CancelFunc // key: "platform_userID", used by the stop command to interrupt a task
	wecomReplay          map[string]time.Time
	pendingConfirmations map[string]robotPendingConfirmation
	alertWake            chan struct{}
	audit                *audit.Service
}

// NewRobotHandler creates a bot handler
func NewRobotHandler(cfg *config.Config, db *database.DB, agentHandler *AgentHandler, logger *zap.Logger) *RobotHandler {
	return &RobotHandler{
		config:               cfg,
		db:                   db,
		agentHandler:         agentHandler,
		logger:               logger,
		sessions:             make(map[string]string),
		sessionRoles:         make(map[string]string),
		sessionModes:         make(map[string]string),
		runningCancels:       make(map[string]context.CancelFunc),
		wecomReplay:          make(map[string]time.Time),
		pendingConfirmations: make(map[string]robotPendingConfirmation),
		alertWake:            make(chan struct{}, 1),
	}
}

func (h *RobotHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

func (h *RobotHandler) acceptFreshWecomRequest(timestamp, nonce, signature string) bool {
	unixSeconds, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return false
	}
	now := time.Now()
	requestTime := time.Unix(unixSeconds, 0)
	if requestTime.Before(now.Add(-5*time.Minute)) || requestTime.After(now.Add(5*time.Minute)) {
		return false
	}
	key := strings.TrimSpace(timestamp) + "\x00" + strings.TrimSpace(nonce) + "\x00" + strings.TrimSpace(signature)
	if strings.TrimSpace(nonce) == "" || strings.TrimSpace(signature) == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for replayKey, seenAt := range h.wecomReplay {
		if now.Sub(seenAt) > 10*time.Minute {
			delete(h.wecomReplay, replayKey)
		}
	}
	if _, exists := h.wecomReplay[key]; exists {
		return false
	}
	h.wecomReplay[key] = now
	return true
}

// sessionKey generates the session key
func (h *RobotHandler) sessionKey(platform, userID string) string {
	return platform + "_" + userID
}

func normalizeRobotBindingCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}

func hashRobotBindingCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRobotBindingCode(code)))
	return fmt.Sprintf("%x", sum[:])
}

func (h *RobotHandler) resolveRobotAccess(platform, userID string) (*database.RBACAccess, error) {
	if h.db == nil {
		return nil, fmt.Errorf("robot auth service unavailable")
	}
	authorization := h.config.Robots.AuthorizationFor(platform)
	var access *database.RBACAccess
	var err error
	switch authorization.EffectiveMode() {
	case config.RobotAuthModeUserBinding:
		access, err = h.db.ResolveRobotRBACAccess(platform, userID)
	case config.RobotAuthModeServiceAccount:
		if !authorization.ExternalUserAllowed(userID) {
			return nil, fmt.Errorf("robot sender is not on the service account whitelist")
		}
		access, err = h.db.ResolveRBACAccess(strings.TrimSpace(authorization.ServiceUserID))
	default:
		return nil, fmt.Errorf("invalid robot auth mode")
	}
	if err != nil {
		return nil, err
	}
	if !access.User.Enabled {
		return nil, fmt.Errorf("the bound platform account has been disabled")
	}
	return access, nil
}

func robotPrincipal(access *database.RBACAccess) authctx.Principal {
	if access == nil {
		return authctx.Principal{}
	}
	return authctx.NewPrincipalWithScopes(access.User.ID, access.User.Username, access.Scope, access.Permissions, access.PermissionScopes)
}

func (h *RobotHandler) robotAccessDeniedMessage(platform string) string {
	if h.config.Robots.AuthorizationFor(platform).EffectiveMode() == config.RobotAuthModeServiceAccount {
		return "The current platform account is not on this bot's service account whitelist, or the service account is unavailable."
	}
	return "The current platform account is not yet bound to a HodaEngineAI user. Please generate a binding code on the web first, then send: bind XXXX-XXXX"
}

func (h *RobotHandler) loadSessionBinding(sk string) (convID, role, agentMode string) {
	if h.db == nil || strings.TrimSpace(sk) == "" {
		return "", "", ""
	}
	binding, err := h.db.GetRobotSessionBinding(sk)
	if err != nil {
		h.logger.Warn("failed to read robot session binding", zap.String("session_key", sk), zap.Error(err))
		return "", "", ""
	}
	if binding == nil {
		return "", "", ""
	}
	return binding.ConversationID, binding.RoleName, binding.AgentMode
}

func (h *RobotHandler) persistSessionBinding(sk, convID, role, agentMode string) {
	if h.db == nil || strings.TrimSpace(sk) == "" || strings.TrimSpace(convID) == "" {
		return
	}
	if err := h.db.UpsertRobotSessionBinding(sk, convID, role, agentMode); err != nil {
		h.logger.Warn("failed to write robot session binding", zap.String("session_key", sk), zap.Error(err))
	}
}

func (h *RobotHandler) deleteSessionBinding(sk string) {
	if h.db == nil || strings.TrimSpace(sk) == "" {
		return
	}
	if err := h.db.DeleteRobotSessionBinding(sk); err != nil {
		h.logger.Warn("failed to delete robot session binding", zap.String("session_key", sk), zap.Error(err))
	}
}

// getOrCreateConversation gets or creates the current conversation; title is used as the title for a new conversation (the first 50 characters of the user's first message)
func (h *RobotHandler) getOrCreateConversation(platform, userID, title string, access *database.RBACAccess) (convID string, isNew bool) {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	convID = h.sessions[sk]
	h.mu.RUnlock()
	ownerID := access.User.ID
	readScope := robotPrincipal(access).ScopeFor("chat:read")
	if convID != "" && access.Permissions["chat:read"] && h.db.UserCanAccessResource(ownerID, readScope, "conversation", convID) {
		return convID, false
	}
	if persistedConvID, persistedRole, persistedMode := h.loadSessionBinding(sk); strings.TrimSpace(persistedConvID) != "" {
		if !access.Permissions["chat:read"] || !h.db.UserCanAccessResource(ownerID, readScope, "conversation", persistedConvID) {
			h.deleteSessionBinding(sk)
		} else {
			// Session binding persistence: the current conversation and role can be restored after a service restart too.
			h.mu.Lock()
			h.sessions[sk] = persistedConvID
			if strings.TrimSpace(persistedRole) != "" {
				h.sessionRoles[sk] = persistedRole
			}
			if strings.TrimSpace(persistedMode) != "" {
				h.sessionModes[sk] = config.NormalizeAgentMode(persistedMode)
			}
			h.mu.Unlock()
			return persistedConvID, false
		}
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = "New chat " + time.Now().Format("01-02 15:04")
	} else {
		t = safeTruncateString(t, 50)
	}
	meta := database.ConversationCreateMeta{Source: "robot:" + platform}
	if !access.Permissions["chat:write"] {
		return "", false
	}
	meta.ProjectID = effectiveProjectID(h.config, "")
	if meta.ProjectID != "" && (!access.Permissions["project:read"] || !h.db.UserCanAccessResource(ownerID, robotPrincipal(access).ScopeFor("project:read"), "project", meta.ProjectID)) {
		meta.ProjectID = ""
	}
	conv, err := h.db.CreateConversation(t, meta)
	if err != nil {
		h.logger.Warn("failed to create robot conversation", zap.Error(err))
		return "", false
	}
	convID = conv.ID
	_ = h.db.SetResourceOwner("conversation", convID, ownerID)
	h.mu.Lock()
	role := h.sessionRoles[sk]
	agentMode := h.sessionModes[sk]
	h.sessions[sk] = convID
	h.mu.Unlock()
	if agentMode == "" {
		agentMode = config.NormalizeRobotAgentMode(h.config.MultiAgent)
	}
	h.persistSessionBinding(sk, convID, role, agentMode)
	return convID, true
}

// setConversation switches the current conversation
func (h *RobotHandler) setConversation(platform, userID, convID string) {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	role := h.sessionRoles[sk]
	agentMode := h.sessionModes[sk]
	h.sessions[sk] = convID
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, role, agentMode)
}

// getRole returns the role used by the current user; returns the default role when none is set
func (h *RobotHandler) getRole(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	role := h.sessionRoles[sk]
	h.mu.RUnlock()
	if strings.TrimSpace(role) != "" {
		return role
	}
	if _, persistedRole, _ := h.loadSessionBinding(sk); strings.TrimSpace(persistedRole) != "" {
		h.mu.Lock()
		h.sessionRoles[sk] = persistedRole
		h.mu.Unlock()
		return persistedRole
	}
	return defaultRoleName
}

// setRole sets the role used by the current user
func (h *RobotHandler) setRole(platform, userID, roleName string) {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	h.sessionRoles[sk] = roleName
	convID := h.sessions[sk]
	agentMode := h.sessionModes[sk]
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, roleName, agentMode)
}

func (h *RobotHandler) getAgentMode(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	mode := h.sessionModes[sk]
	h.mu.RUnlock()
	if mode != "" {
		return config.NormalizeAgentMode(mode)
	}
	if _, _, persistedMode := h.loadSessionBinding(sk); persistedMode != "" {
		mode = config.NormalizeAgentMode(persistedMode)
		h.mu.Lock()
		h.sessionModes[sk] = mode
		h.mu.Unlock()
		return mode
	}
	return config.NormalizeRobotAgentMode(h.config.MultiAgent)
}

func (h *RobotHandler) setAgentMode(platform, userID, mode string) {
	sk := h.sessionKey(platform, userID)
	mode = config.NormalizeAgentMode(mode)
	h.mu.Lock()
	h.sessionModes[sk] = mode
	convID := h.sessions[sk]
	role := h.sessionRoles[sk]
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, role, mode)
}

// clearConversation clears the current conversation (switches to a new one)
func (h *RobotHandler) clearConversation(platform, userID string, access *database.RBACAccess) (newConvID string) {
	title := "New chat " + time.Now().Format("01-02 15:04")
	meta := database.ConversationCreateMeta{Source: "robot:" + platform + ":new"}
	meta.ProjectID = effectiveProjectID(h.config, "")
	ownerID := access.User.ID
	if meta.ProjectID != "" && (!access.Permissions["project:read"] || !h.db.UserCanAccessResource(ownerID, robotPrincipal(access).ScopeFor("project:read"), "project", meta.ProjectID)) {
		meta.ProjectID = ""
	}
	conv, err := h.db.CreateConversation(title, meta)
	if err != nil {
		h.logger.Warn("failed to create new conversation", zap.Error(err))
		return ""
	}
	_ = h.db.SetResourceOwner("conversation", conv.ID, ownerID)
	h.setConversation(platform, userID, conv.ID)
	return conv.ID
}

// HandleMessage processes user input and returns the reply text (called by each platform's webhook)
func (h *RobotHandler) HandleMessage(platform, userID, text string) (reply string) {
	platform = strings.TrimSpace(platform)
	userID = strings.TrimSpace(userID)
	text = strings.TrimSpace(text)
	if platform == "" {
		platform = "unknown"
	}
	if userID == "" {
		h.logger.Warn("robot message is missing a user identifier, rejected", zap.String("platform", platform))
		return "Could not identify the sender; please check the bot's event subscription permissions (a usable user ID must be returned)."
	}
	if text == "" {
		return "Please enter some content or send \"help\" to view the commands."
	}

	// First try to handle it as a command (Chinese and English both supported)
	if cmdReply, ok := h.handleRobotCommand(platform, userID, text); ok {
		return cmdReply
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return h.robotAccessDeniedMessage(platform)
	}
	if !access.Permissions["agent:execute"] || !access.Permissions["chat:read"] || !access.Permissions["chat:write"] {
		return "Insufficient permissions: robot conversations require the agent:execute, chat:read, and chat:write permissions."
	}
	if h.audit != nil && h.config.Robots.AuthorizationFor(platform).EffectiveMode() == config.RobotAuthModeServiceAccount {
		hint := sha256.Sum256([]byte(userID))
		h.audit.RecordSystem(audit.Entry{
			Category: "robot", Action: "service_account_execute", Result: "success", Actor: access.User.Username,
			ResourceType: "robot_sender", ResourceID: platform + ":" + fmt.Sprintf("%x", hint[:4]),
			Message: "whitelisted platform sender used the robot service account to run the Agent",
		})
	}

	// Regular message: route to the Agent
	convID, _ := h.getOrCreateConversation(platform, userID, text, access)
	if convID == "" {
		return "Could not create or retrieve the conversation; please try again later."
	}
	// If the conversation title has the "New chat xx:xx" format (created by the "new" command), update the title to the first message's content, matching the web experience
	if conv, err := h.db.GetConversation(convID); err == nil && strings.HasPrefix(conv.Title, "New chat ") {
		newTitle := safeTruncateString(text, 50)
		if newTitle != "" {
			_ = h.db.UpdateConversationTitle(convID, newTitle)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.robotMessageTimeout())
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	h.runningCancels[sk] = cancel
	h.cancelMu.Unlock()
	defer func() {
		cancel()
		h.cancelMu.Lock()
		delete(h.runningCancels, sk)
		h.cancelMu.Unlock()
	}()
	role := h.getRole(platform, userID)
	agentMode := h.getAgentMode(platform, userID)
	resp, newConvID, err := h.agentHandler.ProcessMessageForRobot(ctx, platform, robotPrincipal(access), convID, text, role, agentMode)
	if err != nil {
		h.logger.Warn("robot Agent execution failed", zap.String("platform", platform), zap.String("userID", userID), zap.Error(err))
		if errors.Is(err, context.Canceled) {
			return "The task has been canceled."
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "The task timed out; please retry later or narrow the scope of this request."
		}
		return "Processing failed: " + err.Error()
	}
	if newConvID != convID {
		h.setConversation(platform, userID, newConvID)
	}
	return resp
}

func (h *RobotHandler) robotMessageTimeout() time.Duration {
	// Timeout for a whole robot message-processing pass (decoupled from the per-tool timeout agent.tool_timeout_minutes).
	return 10 * time.Hour
}

func (h *RobotHandler) cmdHelp(platform, userID string) string {
	access, _ := h.resolveRobotAccess(platform, userID)
	can := func(permission string) bool {
		return access != nil && access.Permissions[permission]
	}
	var b strings.Builder
	b.WriteString("[HodaEngineAI Bot Commands]\n\n")
	b.WriteString("[General]\n")
	b.WriteString("· help — show this help\n")
	b.WriteString("· version — show the current version number\n")
	b.WriteString("· bind <code> — bind a web RBAC user\n")
	b.WriteString("· unbind — request to unbind the account (confirmation required)\n")
	b.WriteString("· whoami — show the platform sender, auth mode, and current effective RBAC identity\n")
	if can("chat:read") || can("chat:write") || can("chat:delete") {
		b.WriteString("\n[Conversation]\n")
		if can("chat:read") {
			b.WriteString("· list — list all conversation titles and IDs\n· switch <ID> — continue a specific conversation\n· status — summarize the current selection\n· task — view the current task status\n")
		}
		if can("chat:write") {
			b.WriteString("· new; clear — start a new conversation\n· rename <name> — change the current conversation's title\n")
		}
		if can("chat:delete") {
			b.WriteString("· delete <ID> — delete the specified conversation (confirmation required)\n")
		}
	}
	if can("roles:read") {
		b.WriteString("\n[Role]\n· roles — list all available roles\n· role <name> — switch the current role\n")
	}
	if can("agent:execute") {
		b.WriteString("\n[Mode]\n· modes — list conversation modes and the current selection\n· mode <name> — switch the conversation mode\n· stop — interrupt the current task\n")
	}
	if can("vulnerability:read") {
		b.WriteString("\n[Vulnerability alerts]\n· vuln alerts — view the subscription status\n· vuln alerts on — enable alerts\n· vuln alerts critical|high|medium — set the minimum level\n· vuln alerts off — disable alerts\n")
	}
	b.WriteString("\n[Diagnostics]\n")
	b.WriteString("· permissions — view current business permissions\n")
	if can("config:read") {
		b.WriteString("· doctor — check key bot configuration status\n")
	}
	b.WriteString("· confirm; cancel — handle confirmation of high-risk operations\n")
	if h.projectsEnabled() && (can("project:read") || can("project:write")) {
		b.WriteString("\n[Project]\n")
		if can("project:read") {
			b.WriteString("· projects — list all projects\n")
		}
		if can("project:write") {
			b.WriteString("· new project <name> — create and bind the current conversation\n· bind project <ID|name> — bind an existing project\n· unbind project — unbind the project\n")
		}
	}
	b.WriteString("\n──────────────\n")
	b.WriteString("For anything other than the commands above, your message will be sent directly to the AI for penetration testing/security analysis.")
	return b.String()
}

func (h *RobotHandler) projectsEnabled() bool {
	return h.config != nil && h.config.Project.Enabled
}

func (h *RobotHandler) resolveProjectByIDOrName(access *database.RBACAccess, idOrName string) (*database.Project, string) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return nil, "Please specify a project ID or name, e.g.: bind project xxx-xxx"
	}
	ownerID := access.User.ID
	scope := robotPrincipal(access).ScopeFor("project:read")
	if p, err := h.db.GetProject(idOrName); err == nil {
		if h.db.UserCanAccessResource(ownerID, scope, "project", p.ID) {
			return p, ""
		}
		return nil, "The project does not exist or you don't have access."
	}
	list, err := h.db.ListProjectsForAccess("", "", 200, 0, ownerID, scope)
	if err != nil {
		return nil, "Failed to query projects: " + err.Error()
	}
	var matches []*database.Project
	for _, p := range list {
		if p.Name == idOrName {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Sprintf("Project \"%s\" does not exist. Send \"projects\" to view the list.", idOrName)
	case 1:
		return matches[0], ""
	default:
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Name \"%s\" matched multiple projects; please bind using the ID:\n", idOrName))
		for _, p := range matches {
			b.WriteString(fmt.Sprintf("· %s\n  ID: %s\n", p.Name, p.ID))
		}
		return nil, strings.TrimSuffix(b.String(), "\n")
	}
}

func (h *RobotHandler) formatProjectLabel(projectID string) string {
	if strings.TrimSpace(projectID) == "" {
		return "not bound"
	}
	if p, err := h.db.GetProject(projectID); err == nil {
		return fmt.Sprintf("\"%s\" (%s)", p.Name, p.ID)
	}
	return projectID
}

func (h *RobotHandler) cmdProjects(platform, userID string) string {
	if !h.projectsEnabled() {
		return "The project feature is not enabled (config.project.enabled)."
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "The current platform account is not yet bound."
	}
	list, err := h.db.ListProjectsForAccess("", "", 50, 0, access.User.ID, robotPrincipal(access).ScopeFor("project:read"))
	if err != nil {
		return "Failed to get the project list: " + err.Error()
	}
	if len(list) == 0 {
		return "No projects yet. Send \"new project <name>\" to create one and bind it to the current conversation."
	}
	var b strings.Builder
	b.WriteString("[Project list]\n")
	for i, p := range list {
		if i >= 20 {
			b.WriteString("… showing only the first 20\n")
			break
		}
		status := p.Status
		if status == "" {
			status = "active"
		}
		b.WriteString(fmt.Sprintf("· %s [%s]\n  ID: %s\n", p.Name, status, p.ID))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdBindProject(platform, userID, idOrName string) string {
	if !h.projectsEnabled() {
		return "The project feature is not enabled (config.project.enabled)."
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "The current platform account is not yet bound."
	}
	p, errMsg := h.resolveProjectByIDOrName(access, idOrName)
	if p == nil {
		return errMsg
	}
	convID, _ := h.getOrCreateConversation(platform, userID, "", access)
	if convID == "" {
		return "Could not get the current conversation; please try again later."
	}
	if err := h.db.SetConversationProjectID(convID, p.ID); err != nil {
		return "Binding failed: " + err.Error()
	}
	return fmt.Sprintf("The current conversation has been bound to project \"%s\"\nID: %s", p.Name, p.ID)
}

func (h *RobotHandler) cmdNewProject(platform, userID, name string) string {
	if !h.projectsEnabled() {
		return "The project feature is not enabled (config.project.enabled)."
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "Please specify a project name, e.g.: new project some-target-pentest"
	}
	access, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "The current platform account is not yet bound."
	}
	p := &database.Project{Name: name, Status: "active"}
	created, err := h.db.CreateProject(p)
	if err != nil {
		return "Failed to create the project: " + err.Error()
	}
	_ = h.db.SetResourceOwner("project", created.ID, access.User.ID)
	convID, _ := h.getOrCreateConversation(platform, userID, name, access)
	if convID == "" {
		return fmt.Sprintf("Project created: \"%s\"\nID: %s\n(failed to bind the current conversation; please manually send \"bind project %s\")", created.Name, created.ID, created.ID)
	}
	if err := h.db.SetConversationProjectID(convID, created.ID); err != nil {
		return fmt.Sprintf("Project created: \"%s\"\nID: %s\nBinding failed: %s", created.Name, created.ID, err.Error())
	}
	return fmt.Sprintf("Project created and bound to the current conversation: \"%s\"\nID: %s", created.Name, created.ID)
}

func (h *RobotHandler) cmdUnbindProject(platform, userID string) string {
	if !h.projectsEnabled() {
		return "The project feature is not enabled (config.project.enabled)."
	}
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	convID := h.sessions[sk]
	h.mu.RUnlock()
	if convID == "" {
		if persistedConvID, _, _ := h.loadSessionBinding(sk); persistedConvID != "" {
			convID = persistedConvID
		}
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "The current platform account is not yet bound."
	}
	if !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:write"), "conversation", convID) {
		return "The conversation does not exist or you don't have access."
	}
	if convID == "" {
		return "There is no conversation in progress, so there is nothing to unbind."
	}
	projectID, err := h.db.GetConversationProjectID(convID)
	if err != nil {
		return "Failed to get the conversation's project: " + err.Error()
	}
	if strings.TrimSpace(projectID) == "" {
		return "The current conversation is not bound to a project."
	}
	if err := h.db.SetConversationProjectID(convID, ""); err != nil {
		return "Failed to unbind: " + err.Error()
	}
	return "The current conversation's project binding has been removed."
}

func (h *RobotHandler) cmdList(platform, userID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "The current platform account is not yet bound."
	}
	convs, err := h.db.ListConversationsForAccess(50, 0, "", "", "", access.User.ID, robotPrincipal(access).ScopeFor("chat:read"))
	if err != nil {
		return "Failed to get the conversation list: " + err.Error()
	}
	if len(convs) == 0 {
		return "No conversations yet. Send anything to automatically create a new conversation."
	}
	var b strings.Builder
	b.WriteString("[Conversation list]\n")
	for i, c := range convs {
		if i >= 20 {
			b.WriteString("… showing only the first 20\n")
			break
		}
		b.WriteString(fmt.Sprintf("· %s\n  ID: %s\n", c.Title, c.ID))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitch(platform, userID, convID string) string {
	if convID == "" {
		return "Please specify a conversation ID, e.g.: switch xxx-xxx-xxx"
	}
	access, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "The current platform account is not yet bound."
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil || !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:read"), "conversation", convID) {
		return "The conversation does not exist or the ID is wrong."
	}
	h.setConversation(platform, userID, conv.ID)
	return fmt.Sprintf("Switched to conversation: \"%s\"\nID: %s", conv.Title, conv.ID)
}

func (h *RobotHandler) cmdNew(platform, userID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "The current platform account is not yet bound."
	}
	newID := h.clearConversation(platform, userID, access)
	if newID == "" {
		return "Failed to create a new conversation; please try again."
	}
	return "A new conversation has started; you can send content directly."
}

func (h *RobotHandler) cmdClear(platform, userID string) string {
	return h.cmdNew(platform, userID)
}

func (h *RobotHandler) cmdStop(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	cancel, ok := h.runningCancels[sk]
	if ok {
		delete(h.runningCancels, sk)
		cancel()
	}
	h.cancelMu.Unlock()
	if !ok {
		return "There is no task currently running."
	}
	return "The current task has been stopped."
}

func (h *RobotHandler) cmdStatus(platform, userID string) string {
	convID := h.currentConversationID(platform, userID)
	if convID == "" {
		return fmt.Sprintf("[Current status]\nCurrent conversation: none\nCurrent role: %s\nCurrent mode: %s\nCurrent project: none\n\nSend anything to create a new conversation.", h.getRole(platform, userID), robotAgentModeLabel(h.getAgentMode(platform, userID)))
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "The current platform account is not yet bound."
	}
	if !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:read"), "conversation", convID) {
		return "The current conversation does not exist or you don't have access."
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil {
		return "Current conversation ID: " + convID + " (failed to get title)"
	}
	role := h.getRole(platform, userID)
	reply := fmt.Sprintf("[Current status]\nCurrent conversation: %s\nConversation ID: %s\nCurrent mode: %s\nCurrent role: %s", conv.Title, conv.ID, robotAgentModeLabel(h.getAgentMode(platform, userID)), role)
	if h.projectsEnabled() {
		projectID, _ := h.db.GetConversationProjectID(conv.ID)
		reply += "\nCurrent project: " + h.formatProjectLabel(projectID)
	} else {
		reply += "\nCurrent project: not enabled"
	}
	return reply
}

func (h *RobotHandler) currentConversationID(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	convID := h.sessions[sk]
	h.mu.RUnlock()
	if convID != "" {
		return convID
	}
	persistedConvID, persistedRole, persistedMode := h.loadSessionBinding(sk)
	if persistedConvID == "" {
		return ""
	}
	h.mu.Lock()
	h.sessions[sk] = persistedConvID
	h.sessionRoles[sk] = persistedRole
	h.sessionModes[sk] = config.NormalizeAgentMode(persistedMode)
	h.mu.Unlock()
	return persistedConvID
}

func (h *RobotHandler) cmdTask(platform, userID string) string {
	convID := h.currentConversationID(platform, userID)
	if convID == "" {
		return "[Task status]\nThere is no conversation and no task currently running."
	}
	if h.agentHandler == nil || h.agentHandler.tasks == nil {
		return "The task status service is unavailable."
	}
	task := h.agentHandler.tasks.GetTaskSnapshot(convID)
	if task == nil {
		return "[Task status]\nStatus: idle\nThere is no task currently running."
	}
	elapsed := time.Since(task.StartedAt).Round(time.Second)
	return fmt.Sprintf("[Task status]\nStatus: %s\nRunning for: %s\nConversation ID: %s\nMode: %s\nAvailable action: stop", task.Status, elapsed, convID, robotAgentModeLabel(h.getAgentMode(platform, userID)))
}

func (h *RobotHandler) cmdRename(platform, userID, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Please specify a new title, e.g.: rename external-asset-survey"
	}
	title = safeTruncateString(title, 100)
	convID := h.currentConversationID(platform, userID)
	if convID == "" {
		return "There is no conversation, so it cannot be renamed."
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil || !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:write"), "conversation", convID) {
		return "The current conversation does not exist or you don't have permission to modify it."
	}
	if err := h.db.UpdateConversationTitle(convID, title); err != nil {
		return "Rename failed: " + err.Error()
	}
	h.recordRobotCommandAudit(access, platform, "conversation_rename", "conversation", convID, "bot renamed the current conversation")
	return fmt.Sprintf("The current conversation has been renamed to: \"%s\"", title)
}

func (h *RobotHandler) cmdPermissions(platform, userID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return h.robotAccessDeniedMessage(platform)
	}
	allowed := func(permission string) string {
		if access.Permissions[permission] {
			return "allowed"
		}
		return "not allowed"
	}
	return fmt.Sprintf("[Current permissions]\nExecute Agent: %s\nRead conversations: %s\nEdit conversations: %s\nDelete conversations: %s\nRead roles: %s\nRead projects: %s\nEdit projects: %s\nResource scope: %s", allowed("agent:execute"), allowed("chat:read"), allowed("chat:write"), allowed("chat:delete"), allowed("roles:read"), allowed("project:read"), allowed("project:write"), access.Scope)
}

func (h *RobotHandler) cmdDoctor() string {
	configured := func(ok bool) string {
		if ok {
			return "OK"
		}
		return "not configured"
	}
	enabled := func(ok bool) string {
		if ok {
			return "enabled"
		}
		return "disabled"
	}
	enabledInternalTools := 0
	for _, tool := range h.config.Security.Tools {
		if tool.Enabled {
			enabledInternalTools++
		}
	}
	enabledExternal := 0
	for _, server := range h.config.ExternalMCP.Servers {
		if server.ExternalMCPEnable && !server.Disabled {
			enabledExternal++
		}
	}
	return fmt.Sprintf("[Config diagnostics]\nMain model: %s\nAgent multi-agent: %s\nBuilt-in MCP tools: %d/%d enabled\nHTTP MCP service: %s\nExternal MCP: %d enabled\nKnowledge base: %s\nProjects: %s\nNote: built-in tools do not depend on the HTTP MCP service; this command only checks configuration and does not probe external services.", configured(strings.TrimSpace(h.config.OpenAI.Model) != "" && strings.TrimSpace(h.config.OpenAI.BaseURL) != ""), enabled(h.config.MultiAgent.Enabled), enabledInternalTools, len(h.config.Security.Tools), enabled(h.config.MCP.Enabled), enabledExternal, enabled(h.config.Knowledge.Enabled), enabled(h.config.Project.Enabled))
}

func (h *RobotHandler) recordRobotCommandAudit(access *database.RBACAccess, platform, action, resourceType, resourceID, message string) {
	if h.audit == nil || access == nil {
		return
	}
	h.audit.RecordSystem(audit.Entry{Category: "robot", Action: action, Result: "success", Actor: access.User.Username, ResourceType: resourceType, ResourceID: resourceID, Message: message + " (" + platform + ")"})
}

func (h *RobotHandler) cmdRoles() string {
	if h.config.Roles == nil || len(h.config.Roles) == 0 {
		return "No roles are available yet."
	}
	names := make([]string, 0, len(h.config.Roles))
	for name, role := range h.config.Roles {
		if role.Enabled {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "No roles are available yet."
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == defaultRoleName {
			return true
		}
		if names[j] == defaultRoleName {
			return false
		}
		return names[i] < names[j]
	})
	var b strings.Builder
	b.WriteString("[Role list]\n")
	for _, name := range names {
		role := h.config.Roles[name]
		desc := role.Description
		if desc == "" {
			desc = "no description"
		}
		b.WriteString(fmt.Sprintf("· %s — %s\n", name, desc))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitchRole(platform, userID, roleName string) string {
	if roleName == "" {
		return "Please specify a role name, e.g.: role pentest"
	}
	if h.config.Roles == nil {
		return "No roles are available yet."
	}
	role, exists := h.config.Roles[roleName]
	if !exists {
		return fmt.Sprintf("Role \"%s\" does not exist. Send \"roles\" to view available roles.", roleName)
	}
	if !role.Enabled {
		return fmt.Sprintf("Role \"%s\" is disabled.", roleName)
	}
	h.setRole(platform, userID, roleName)
	return fmt.Sprintf("Switched to role: \"%s\"\n%s", roleName, role.Description)
}

func robotAgentModeLabel(mode string) string {
	switch config.NormalizeAgentMode(mode) {
	case "deep":
		return "Deep"
	case "plan_execute":
		return "Plan-Execute"
	case "supervisor":
		return "Supervisor"
	default:
		return "Agent single-agent"
	}
}

func parseRobotAgentMode(input string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "single", "pi-single", "pi_single", "eino_single", "eino-single", "单代理", "eino单代理", "eino 单代理":
		return "single", true
	case "deep":
		return "deep", true
	case "plan_execute", "plan-execute", "planexecute", "pe":
		return "plan_execute", true
	case "supervisor", "super", "sv":
		return "supervisor", true
	default:
		return "", false
	}
}

func (h *RobotHandler) cmdModes(platform, userID string) string {
	current := h.getAgentMode(platform, userID)
	multiStatus := "available"
	if h.config == nil || !h.config.MultiAgent.Enabled {
		multiStatus = "unavailable (enable Agent multi-agent in system settings)"
	}
	return fmt.Sprintf("[Conversation modes]\n· Agent single-agent — available\n· Deep — %s\n· Plan-Execute — %s\n· Supervisor — %s\n\nCurrent mode: %s\nSwitch example: mode deep", multiStatus, multiStatus, multiStatus, robotAgentModeLabel(current))
}

func (h *RobotHandler) cmdSwitchMode(platform, userID, input string) string {
	mode, ok := parseRobotAgentMode(input)
	if !ok {
		return fmt.Sprintf("Unsupported conversation mode \"%s\". Send \"modes\" to view the available modes.", strings.TrimSpace(input))
	}
	if mode != "single" && (h.config == nil || !h.config.MultiAgent.Enabled) {
		return fmt.Sprintf("Cannot switch to %s: enable Agent multi-agent in system settings first.", robotAgentModeLabel(mode))
	}
	h.setAgentMode(platform, userID, mode)
	return fmt.Sprintf("Conversation mode switched to: %s\nSubsequent messages and new conversations will use this mode.", robotAgentModeLabel(mode))
}

func (h *RobotHandler) cmdDelete(platform, userID, convID string) string {
	if convID == "" {
		return "Please specify a conversation ID, e.g.: delete xxx-xxx-xxx"
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "The current platform account is not yet bound."
	}
	if !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:delete"), "conversation", convID) {
		return "The conversation does not exist or you don't have access."
	}
	h.setPendingConfirmation(platform, userID, "delete_conversation", convID)
	return fmt.Sprintf("⚠️ About to delete conversation ID: %s\nThis action cannot be undone. Send \"confirm\" within 2 minutes to proceed, or send \"cancel\".", convID)
}

func (h *RobotHandler) executeDelete(platform, userID, convID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil || !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:delete"), "conversation", convID) {
		return "The conversation does not exist or you don't have permission to delete it."
	}
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	currentConvID := h.sessions[sk]
	h.mu.RUnlock()
	if convID == currentConvID {
		// When deleting the current conversation, clear the session binding first
		h.mu.Lock()
		delete(h.sessions, sk)
		delete(h.sessionRoles, sk)
		delete(h.sessionModes, sk)
		h.mu.Unlock()
		h.deleteSessionBinding(sk)
	}
	if h.agentHandler != nil {
		h.agentHandler.CancelRunningTaskForConversation(convID)
	}
	if err := h.db.DeleteConversation(convID); err != nil {
		return "Delete failed: " + err.Error()
	}
	h.recordRobotCommandAudit(access, platform, "conversation_delete", "conversation", convID, "bot deleted the conversation")
	return fmt.Sprintf("Deleted conversation ID: %s", convID)
}

func (h *RobotHandler) cmdVersion() string {
	v := h.config.Version
	if v == "" {
		v = "unknown"
	}
	return "HodaEngineAI " + v
}

func (h *RobotHandler) cmdIdentity(platform, userID string) string {
	authorization := h.config.Robots.AuthorizationFor(platform)
	mode := authorization.EffectiveMode()
	modeLabel := "per-user binding (user_binding)"
	if mode == config.RobotAuthModeServiceAccount {
		modeLabel = "dedicated service account (service_account)"
	}
	var b strings.Builder
	b.WriteString("[Bot identity]\n")
	b.WriteString("Platform: " + platform + "\n")
	b.WriteString("Sender ID: " + userID + "\n")
	b.WriteString("Auth mode: " + modeLabel + "\n")

	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		if mode == config.RobotAuthModeServiceAccount {
			b.WriteString("Auth status: denied (the sender is not on the whitelist, or the service account is unavailable)")
		} else {
			b.WriteString("Auth status: not bound\n")
			b.WriteString("Hint: please generate a binding code on the web, then send \"bind XXXX-XXXX\"")
		}
		return b.String()
	}

	name := strings.TrimSpace(access.User.DisplayName)
	if name == "" {
		name = access.User.Username
	}
	roleNames := make([]string, 0, len(access.Roles))
	for _, role := range access.Roles {
		roleNames = append(roleNames, role.Name)
	}
	if len(roleNames) == 0 {
		roleNames = append(roleNames, "no role assigned")
	}
	b.WriteString("Auth status: authorized\n")
	b.WriteString("Effective identity: " + name + " (" + access.User.Username + ")\n")
	b.WriteString("RBAC User ID: " + access.User.ID + "\n")
	b.WriteString("Platform roles: " + strings.Join(roleNames, ", ") + "\n")
	b.WriteString("Resource scope: " + access.Scope + "\n")
	b.WriteString(fmt.Sprintf("Effective permissions: %d", len(access.Permissions)))
	return b.String()
}

func robotCommandPermission(text string) (string, bool) {
	switch {
	case text == robotCmdHelp || text == "help" || text == "\uff1f" /* fullwidth ? */ || text == "?", text == robotCmdVersion || text == "version", text == robotCmdIdentity || text == "whoami":
		return "", true
	case text == robotCmdList || text == robotCmdListAlt || text == "list",
		strings.HasPrefix(text, robotCmdSwitch+" "), strings.HasPrefix(text, robotCmdContinue+" "),
		strings.HasPrefix(text, "switch "), strings.HasPrefix(text, "continue "),
		text == robotCmdStatus || text == "status", text == robotCmdTask || text == "task":
		return "chat:read", true
	case text == robotCmdNew || text == "new", text == robotCmdClear || text == "clear",
		strings.HasPrefix(text, robotCmdRename+" "), strings.HasPrefix(text, "rename "):
		return "chat:write", true
	case strings.HasPrefix(text, robotCmdDelete+" "), strings.HasPrefix(text, "delete "):
		return "chat:delete", true
	case text == robotCmdStop || text == "stop":
		return "agent:execute", true
	case text == robotCmdRoles || text == robotCmdRolesList || text == "roles",
		strings.HasPrefix(text, robotCmdRoles+" "), strings.HasPrefix(text, robotCmdSwitchRole+" "), strings.HasPrefix(text, "role "):
		return "roles:read", true
	case text == robotCmdModes || text == robotCmdModesList || text == "modes",
		strings.HasPrefix(text, robotCmdModes+" "), strings.HasPrefix(text, robotCmdSwitchMode+" "), strings.HasPrefix(text, "mode "):
		return "agent:execute", true
	case text == robotCmdPermissions || text == "permissions":
		return "", true
	case text == robotCmdConfirm || text == "confirm", text == robotCmdCancel || text == "cancel":
		return "", true
	case text == robotCmdDoctor || text == "doctor":
		return "config:read", true
	case text == robotCmdProjects || text == robotCmdProjectsList || text == "projects":
		return "project:read", true
	case text == robotCmdVulnAlerts || strings.HasPrefix(text, robotCmdVulnAlerts+" "),
		text == "vuln alerts" || strings.HasPrefix(text, "vuln alerts "):
		return "vulnerability:read", true
	case text == robotCmdUnbindProject || text == "unbind project",
		strings.HasPrefix(text, robotCmdNewProject+" "), strings.HasPrefix(text, "new project "),
		strings.HasPrefix(text, robotCmdBindProject+" "), strings.HasPrefix(text, "bind project "):
		return "project:write", true
	default:
		return "", false
	}
}

func (h *RobotHandler) cmdBindUser(platform, userID, code string) string {
	if h.config.Robots.AuthorizationFor(platform).EffectiveMode() != config.RobotAuthModeUserBinding {
		return "This bot uses a controlled service-account mode and does not accept user binding."
	}
	code = normalizeRobotBindingCode(code)
	if code == "" {
		return "Please provide a binding code, e.g.: bind ABCD-1234"
	}
	user, err := h.db.ConsumeRobotBindingCode(platform, userID, hashRobotBindingCode(code))
	if err != nil {
		return "Binding failed: the binding code is invalid, already used, or expired. Please regenerate it on the web."
	}
	// Never carry an old synthetic-owner conversation into the RBAC identity.
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	delete(h.sessions, sk)
	delete(h.sessionRoles, sk)
	delete(h.sessionModes, sk)
	h.mu.Unlock()
	h.deleteSessionBinding(sk)
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Username
	}
	if h.audit != nil {
		hint := sha256.Sum256([]byte(userID))
		h.audit.RecordSystem(audit.Entry{
			Category: "auth", Action: "robot_bind", Result: "success", Actor: user.Username,
			ResourceType: "robot_binding", ResourceID: platform + ":" + fmt.Sprintf("%x", hint[:4]), Message: "robot platform account bound successfully",
		})
	}
	return fmt.Sprintf("Binding succeeded, current identity: %s. Subsequent actions will use this user's RBAC permissions in real time.", name)
}

func (h *RobotHandler) cmdUnbindUser(platform, userID string) string {
	if h.config.Robots.AuthorizationFor(platform).EffectiveMode() != config.RobotAuthModeUserBinding {
		return "This bot uses a controlled service-account mode and does not require user unbinding."
	}
	_, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "The current platform account is not yet bound."
	}
	h.setPendingConfirmation(platform, userID, "unbind_user", "")
	return "⚠️ About to unbind the current platform account. Send \"confirm\" within 2 minutes to proceed, or send \"cancel\"."
}

func (h *RobotHandler) executeUnbindUser(platform, userID string) string {
	access, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "The current platform account is not yet bound."
	}
	if err := h.db.DeleteRobotIdentityBinding(platform, userID); err != nil {
		return "Unbind failed; please try again later."
	}
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	delete(h.sessions, sk)
	delete(h.sessionRoles, sk)
	delete(h.sessionModes, sk)
	h.mu.Unlock()
	h.deleteSessionBinding(sk)
	if h.audit != nil {
		hint := sha256.Sum256([]byte(userID))
		h.audit.RecordSystem(audit.Entry{
			Category: "auth", Action: "robot_unbind", Result: "success", Actor: access.User.Username,
			ResourceType: "robot_binding", ResourceID: platform + ":" + fmt.Sprintf("%x", hint[:4]), Message: "robot platform account unbound successfully",
		})
	}
	return "The binding between the current platform account and the HodaEngineAI user has been removed."
}

func (h *RobotHandler) setPendingConfirmation(platform, userID, action, target string) {
	sk := h.sessionKey(platform, userID)
	now := time.Now()
	h.mu.Lock()
	for key, pending := range h.pendingConfirmations {
		if now.After(pending.ExpiresAt) {
			delete(h.pendingConfirmations, key)
		}
	}
	h.pendingConfirmations[sk] = robotPendingConfirmation{Action: action, Target: target, ExpiresAt: now.Add(2 * time.Minute)}
	h.mu.Unlock()
}

func (h *RobotHandler) cmdConfirm(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	pending, ok := h.pendingConfirmations[sk]
	delete(h.pendingConfirmations, sk)
	h.mu.Unlock()
	if !ok || time.Now().After(pending.ExpiresAt) {
		return "There is no action pending confirmation, or the confirmation timed out."
	}
	switch pending.Action {
	case "delete_conversation":
		return h.executeDelete(platform, userID, pending.Target)
	case "unbind_user":
		return h.executeUnbindUser(platform, userID)
	default:
		return "The pending action was invalid and has been canceled."
	}
}

func (h *RobotHandler) cmdCancelConfirmation(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	_, ok := h.pendingConfirmations[sk]
	delete(h.pendingConfirmations, sk)
	h.mu.Unlock()
	if !ok {
		return "There is no action pending confirmation."
	}
	return "The pending action has been canceled."
}

// handleRobotCommand processes the bot's built-in commands; if a command is matched it returns (reply text, true), otherwise ("", false)
func (h *RobotHandler) handleRobotCommand(platform, userID, text string) (string, bool) {
	if (strings.HasPrefix(text, robotCmdBindUser+" ") || strings.HasPrefix(text, "bind ")) && !strings.HasPrefix(text, "bind project ") {
		parts := strings.SplitN(text, " ", 2)
		return h.cmdBindUser(platform, userID, strings.TrimSpace(parts[1])), true
	}
	if text == robotCmdUnbindUser || text == "unbind" {
		return h.cmdUnbindUser(platform, userID), true
	}
	if permission, recognized := robotCommandPermission(text); recognized && permission != "" {
		access, err := h.resolveRobotAccess(platform, userID)
		if err != nil {
			return h.robotAccessDeniedMessage(platform), true
		}
		if !access.Permissions[permission] {
			return fmt.Sprintf("Insufficient permissions: missing the %s permission.", permission), true
		}
	}
	switch {
	case text == robotCmdVulnAlerts || text == "vuln alerts":
		return h.cmdVulnerabilityAlerts(platform, userID, ""), true
	case strings.HasPrefix(text, robotCmdVulnAlerts+" "):
		return h.cmdVulnerabilityAlerts(platform, userID, strings.TrimSpace(text[len(robotCmdVulnAlerts)+1:])), true
	case strings.HasPrefix(text, "vuln alerts "):
		return h.cmdVulnerabilityAlerts(platform, userID, strings.TrimSpace(text[len("vuln alerts "):])), true
	case text == robotCmdHelp || text == "help" || text == "\uff1f" /* fullwidth ? */ || text == "?":
		return h.cmdHelp(platform, userID), true
	case text == robotCmdIdentity || text == "whoami":
		return h.cmdIdentity(platform, userID), true
	case text == robotCmdConfirm || text == "confirm":
		return h.cmdConfirm(platform, userID), true
	case text == robotCmdCancel || text == "cancel":
		return h.cmdCancelConfirmation(platform, userID), true
	case text == robotCmdList || text == robotCmdListAlt || text == "list":
		return h.cmdList(platform, userID), true
	case strings.HasPrefix(text, robotCmdSwitch+" ") || strings.HasPrefix(text, robotCmdContinue+" ") || strings.HasPrefix(text, "switch ") || strings.HasPrefix(text, "continue "):
		var id string
		switch {
		case strings.HasPrefix(text, robotCmdSwitch+" "):
			id = strings.TrimSpace(text[len(robotCmdSwitch)+1:])
		case strings.HasPrefix(text, robotCmdContinue+" "):
			id = strings.TrimSpace(text[len(robotCmdContinue)+1:])
		case strings.HasPrefix(text, "switch "):
			id = strings.TrimSpace(text[7:])
		default:
			id = strings.TrimSpace(text[9:])
		}
		return h.cmdSwitch(platform, userID, id), true
	case text == robotCmdNew || text == "new":
		return h.cmdNew(platform, userID), true
	case text == robotCmdClear || text == "clear":
		return h.cmdClear(platform, userID), true
	case text == robotCmdStatus || text == "status":
		return h.cmdStatus(platform, userID), true
	case text == robotCmdTask || text == "task":
		return h.cmdTask(platform, userID), true
	case strings.HasPrefix(text, robotCmdRename+" ") || strings.HasPrefix(text, "rename "):
		var title string
		if strings.HasPrefix(text, robotCmdRename+" ") {
			title = strings.TrimSpace(text[len(robotCmdRename)+1:])
		} else {
			title = strings.TrimSpace(text[len("rename "):])
		}
		return h.cmdRename(platform, userID, title), true
	case text == robotCmdStop || text == "stop":
		return h.cmdStop(platform, userID), true
	case text == robotCmdRoles || text == robotCmdRolesList || text == "roles":
		return h.cmdRoles(), true
	case strings.HasPrefix(text, robotCmdRoles+" ") || strings.HasPrefix(text, robotCmdSwitchRole+" ") || strings.HasPrefix(text, "role "):
		var roleName string
		switch {
		case strings.HasPrefix(text, robotCmdRoles+" "):
			roleName = strings.TrimSpace(text[len(robotCmdRoles)+1:])
		case strings.HasPrefix(text, robotCmdSwitchRole+" "):
			roleName = strings.TrimSpace(text[len(robotCmdSwitchRole)+1:])
		default:
			roleName = strings.TrimSpace(text[5:])
		}
		return h.cmdSwitchRole(platform, userID, roleName), true
	case text == robotCmdModes || text == robotCmdModesList || text == "modes":
		return h.cmdModes(platform, userID), true
	case strings.HasPrefix(text, robotCmdModes+" ") || strings.HasPrefix(text, robotCmdSwitchMode+" ") || strings.HasPrefix(text, "mode "):
		var mode string
		switch {
		case strings.HasPrefix(text, robotCmdModes+" "):
			mode = strings.TrimSpace(text[len(robotCmdModes)+1:])
		case strings.HasPrefix(text, robotCmdSwitchMode+" "):
			mode = strings.TrimSpace(text[len(robotCmdSwitchMode)+1:])
		default:
			mode = strings.TrimSpace(text[5:])
		}
		return h.cmdSwitchMode(platform, userID, mode), true
	case text == robotCmdPermissions || text == "permissions":
		return h.cmdPermissions(platform, userID), true
	case text == robotCmdDoctor || text == "doctor":
		return h.cmdDoctor(), true
	case strings.HasPrefix(text, robotCmdDelete+" ") || strings.HasPrefix(text, "delete "):
		var convID string
		if strings.HasPrefix(text, robotCmdDelete+" ") {
			convID = strings.TrimSpace(text[len(robotCmdDelete)+1:])
		} else {
			convID = strings.TrimSpace(text[7:])
		}
		return h.cmdDelete(platform, userID, convID), true
	case text == robotCmdVersion || text == "version":
		return h.cmdVersion(), true
	case text == robotCmdProjects || text == robotCmdProjectsList || text == "projects":
		return h.cmdProjects(platform, userID), true
	case text == robotCmdUnbindProject || text == "unbind project":
		return h.cmdUnbindProject(platform, userID), true
	case strings.HasPrefix(text, robotCmdNewProject+" ") || strings.HasPrefix(text, "new project "):
		var name string
		if strings.HasPrefix(text, robotCmdNewProject+" ") {
			name = strings.TrimSpace(text[len(robotCmdNewProject)+1:])
		} else {
			name = strings.TrimSpace(text[len("new project "):])
		}
		return h.cmdNewProject(platform, userID, name), true
	case strings.HasPrefix(text, robotCmdBindProject+" ") || strings.HasPrefix(text, "bind project "):
		var idOrName string
		if strings.HasPrefix(text, robotCmdBindProject+" ") {
			idOrName = strings.TrimSpace(text[len(robotCmdBindProject)+1:])
		} else {
			idOrName = strings.TrimSpace(text[len("bind project "):])
		}
		return h.cmdBindProject(platform, userID, idOrName), true
	default:
		return "", false
	}
}

// —————— WeCom ——————

// wecomXML is the WeCom callback XML (the simplified structure for plaintext mode; encrypted mode must be decrypted before parsing)
type wecomXML struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	MsgID        string `xml:"MsgId"`
	AgentID      int64  `xml:"AgentID"`
	Encrypt      string `xml:"Encrypt"` // in encrypted mode, the message is here
}

// wecomReplyXML is the passive-reply XML (kept only for compatibility; the manually constructed XML is used now)
type wecomReplyXML struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
}

// wecomRequireToken: the WeCom callback must have a Token configured; the request is rejected when it's missing, to prevent unauthorized triggering of the Agent.
func (h *RobotHandler) wecomRequireToken(c *gin.Context) (string, bool) {
	token := strings.TrimSpace(h.config.Robots.Wecom.Token)
	if token == "" {
		h.logger.Warn("WeCom is enabled but no token is configured; callback rejected (set robots.wecom.token in the config)")
		c.String(http.StatusForbidden, "")
		return "", false
	}
	return token, true
}

// HandleWecomGET handles the WeCom URL verification (GET)
func (h *RobotHandler) HandleWecomGET(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		c.String(http.StatusNotFound, "")
		return
	}
	token, ok := h.wecomRequireToken(c)
	if !ok {
		return
	}
	// Gin's Query() automatically URL-decodes, so this is already the correct base64 string
	echostr := c.Query("echostr")
	msgSignature := c.Query("msg_signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")

	// Verify the signature: sort the four parameters token, timestamp, nonce, and echostr, concatenate them, then compute SHA1
	signature := h.signWecomRequest(token, timestamp, nonce, echostr)
	if signature != msgSignature {
		h.logger.Warn("WeCom URL verification signature check failed", zap.String("expected", msgSignature), zap.String("got", signature))
		c.String(http.StatusBadRequest, "invalid signature")
		return
	}

	if echostr == "" {
		c.String(http.StatusBadRequest, "missing echostr")
		return
	}

	// If EncodingAESKey is configured, this is encrypted mode, so echostr must be decrypted
	if h.config.Robots.Wecom.EncodingAESKey != "" {
		decrypted, err := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, echostr)
		if err != nil {
			h.logger.Warn("failed to decrypt WeCom echostr", zap.Error(err))
			c.String(http.StatusBadRequest, "decrypt failed")
			return
		}
		c.String(http.StatusOK, string(decrypted))
		return
	}

	// In plaintext mode, just return echostr directly
	c.String(http.StatusOK, echostr)
}

// signWecomRequest generates the WeCom request signature
// WeCom signature algorithm: sort the four values token, timestamp, nonce, and echostr, concatenate them into a string, then compute SHA1
func (h *RobotHandler) signWecomRequest(token, timestamp, nonce, echostr string) string {
	strs := []string{token, timestamp, nonce, echostr}
	sort.Strings(strs)
	s := strings.Join(strs, "")
	hash := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", hash)
}

// wecomDecrypt decrypts a WeCom message (AES-256-CBC, PKCS7, plaintext format: 16 random bytes + 4-byte length + message + corpID)
func wecomDecrypt(encodingAESKey, encryptedB64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encoding_aes_key should decode to 32 bytes")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := key[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length is not a multiple of the block size")
	}
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	// strip PKCS7 padding
	n := int(plain[len(plain)-1])
	if n < 1 || n > 32 {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	plain = plain[:len(plain)-n]
	// WeCom format: 16 random bytes + 4-byte length (big-endian) + message + corpID
	if len(plain) < 20 {
		return nil, fmt.Errorf("plaintext too short")
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	if int(20+msgLen) > len(plain) {
		return nil, fmt.Errorf("message length out of bounds")
	}
	return plain[20 : 20+msgLen], nil
}

// wecomEncrypt encrypts a WeCom message (AES-256-CBC, PKCS7, plaintext format: 16 random bytes + 4-byte length + message + corpID)
func wecomEncrypt(encodingAESKey, message, corpID string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return "", err
	}
	if len(key) != 32 {
		return "", fmt.Errorf("encoding_aes_key should decode to 32 bytes")
	}
	// construct the plaintext: 16 random bytes + 4-byte length (big-endian) + message + corpID
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		// fallback: use a timestamp to generate the random bytes
		for i := range random {
			random[i] = byte(time.Now().UnixNano() % 256)
		}
	}
	msgLen := len(message)
	msgBytes := []byte(message)
	corpBytes := []byte(corpID)
	plain := make([]byte, 16+4+msgLen+len(corpBytes))
	copy(plain[:16], random)
	binary.BigEndian.PutUint32(plain[16:20], uint32(msgLen))
	copy(plain[20:20+msgLen], msgBytes)
	copy(plain[20+msgLen:], corpBytes)
	// PKCS7 padding
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	pad := bytes.Repeat([]byte{byte(padding)}, padding)
	plain = append(plain, pad...)
	// AES-256-CBC encryption
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := key[:16]
	ciphertext := make([]byte, len(plain))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plain)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// HandleWecomPOST handles the WeCom message callback (POST), supporting both plaintext and encrypted modes
func (h *RobotHandler) HandleWecomPOST(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		h.logger.Debug("WeCom bot is not enabled, skipping request")
		c.String(http.StatusOK, "")
		return
	}
	// get the signature parameters from the URL (needed when replying in encrypted mode)
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	msgSignature := c.Query("msg_signature")

	// read the request body first; it is needed for the parsing/signature verification that follows
	bodyRaw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Warn("failed to read the WeCom POST request body", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	h.logger.Debug("WeCom POST request received", zap.String("body", string(bodyRaw)))

	// Verify the request signature to prevent forgery. The WeCom signature algorithm is the same as URL verification, using the four fields token, timestamp, nonce, and Encrypt.
	// When WeCom is enabled, a token must be configured and the signature verified, to prevent unauthorized requests from triggering the Agent.
	token, ok := h.wecomRequireToken(c)
	if !ok {
		return
	}
	if msgSignature == "" {
		h.logger.Warn("WeCom POST is missing a signature, rejected (the callback must include msg_signature)")
		c.String(http.StatusOK, "")
		return
	}
	var tmp wecomXML
	if err := xml.Unmarshal(bodyRaw, &tmp); err != nil {
		h.logger.Warn("failed to parse XML before WeCom POST signature verification", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	expected := h.signWecomRequest(token, timestamp, nonce, tmp.Encrypt)
	if expected != msgSignature {
		h.logger.Warn("WeCom POST signature verification failed", zap.String("expected", expected), zap.String("got", msgSignature))
		c.String(http.StatusOK, "")
		return
	}
	if !h.acceptFreshWecomRequest(timestamp, nonce, msgSignature) {
		h.logger.Warn("WeCom POST timestamp expired or request replayed, rejected")
		c.String(http.StatusOK, "")
		return
	}

	var body wecomXML
	if err := xml.Unmarshal(bodyRaw, &body); err != nil {
		h.logger.Warn("failed to parse WeCom POST XML", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	h.logger.Debug("WeCom XML parsed successfully", zap.String("ToUserName", body.ToUserName), zap.String("FromUserName", body.FromUserName), zap.String("MsgType", body.MsgType), zap.String("Content", body.Content), zap.String("Encrypt", body.Encrypt))

	// save the enterprise ID (used for plaintext-mode replies)
	enterpriseID := body.ToUserName

	// when EncodingAESKey is configured, messages must go through encrypted mode; reject a plaintext XML bypass
	if strings.TrimSpace(h.config.Robots.Wecom.EncodingAESKey) != "" && strings.TrimSpace(body.Encrypt) == "" {
		h.logger.Warn("WeCom is configured for encrypted mode but a plaintext message was received, rejected")
		c.String(http.StatusOK, "")
		return
	}

	// encrypted mode: decrypt first, then parse the inner XML
	if body.Encrypt != "" && h.config.Robots.Wecom.EncodingAESKey != "" {
		h.logger.Debug("WeCom entering encrypted-mode decryption flow")
		decrypted, err := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, body.Encrypt)
		if err != nil {
			h.logger.Warn("failed to decrypt WeCom message", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		h.logger.Debug("WeCom decryption succeeded", zap.String("decrypted", string(decrypted)))
		if err := xml.Unmarshal(decrypted, &body); err != nil {
			h.logger.Warn("failed to parse XML after WeCom decryption", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		h.logger.Debug("WeCom inner XML parsed successfully", zap.String("FromUserName", body.FromUserName), zap.String("Content", body.Content))
	}

	tenantKey := strings.TrimSpace(enterpriseID)
	if tenantKey == "" {
		tenantKey = strings.TrimSpace(h.config.Robots.Wecom.CorpID)
	}
	if tenantKey == "" {
		tenantKey = "default"
	}
	rawUserID := strings.TrimSpace(body.FromUserName)
	replyUserID := rawUserID
	userID := ""
	if rawUserID != "" {
		userID = "t:" + tenantKey + "|u:" + rawUserID
	}
	text := strings.TrimSpace(body.Content)
	if userID == "" {
		h.logger.Warn("WeCom message is missing a usable user identifier, ignored")
		c.String(http.StatusOK, "success")
		return
	}

	// limit the reply content length (WeCom limits it to 2048 bytes)
	maxReplyLen := 2000
	limitReply := func(s string) string {
		if len(s) > maxReplyLen {
			return s[:maxReplyLen] + "\n\n(content too long, truncated)"
		}
		return s
	}

	if body.MsgType != "text" {
		h.logger.Debug("WeCom received a non-text message", zap.String("MsgType", body.MsgType))
		h.sendWecomReply(c, replyUserID, enterpriseID, limitReply("Only text messages are currently supported; please send text."), timestamp, nonce)
		return
	}

	// Text message: first check whether it's a built-in command (such as help/list/new, etc.); these commands are handled quickly, so they can go through the passive reply directly, without depending on the proactive-send API.
	if cmdReply, ok := h.handleRobotCommand("wecom", userID, text); ok {
		h.logger.Debug("WeCom received a command message, using passive reply", zap.String("userID", userID), zap.String("text", text))
		h.sendWecomReply(c, replyUserID, enterpriseID, limitReply(cmdReply), timestamp, nonce)
		return
	}

	h.logger.Debug("WeCom starting to process message (async AI)", zap.String("userID", userID), zap.String("text", text))

	// WeCom's passive reply has a 5-second timeout limit, while AI calls usually take longer than that.
	// The recommended approach is used here: return success (or an empty string) immediately, then push the full reply via the proactive-send API.
	c.String(http.StatusOK, "success")

	// process the message asynchronously and send the result via the WeCom proactive-message API
	go func() {
		reply := h.HandleMessage("wecom", userID, text)
		reply = limitReply(reply)
		h.logger.Debug("WeCom message processing complete", zap.String("userID", userID), zap.String("reply", reply))
		// call the WeCom API to proactively send the message
		h.sendWecomMessageViaAPI(rawUserID, enterpriseID, reply)
	}()
}

// sendWecomReply sends a WeCom reply (automatically encrypted in encrypted mode)
// Parameters: toUser=user ID, fromUser=enterprise ID (plaintext mode) / CorpID (encrypted mode), content=reply content, timestamp/nonce=request parameters
func (h *RobotHandler) sendWecomReply(c *gin.Context, toUser, fromUser, content, timestamp, nonce string) {
	// encrypted mode: check whether EncodingAESKey is configured
	if h.config.Robots.Wecom.EncodingAESKey != "" {
		// encrypted mode uses CorpID for encryption
		corpID := h.config.Robots.Wecom.CorpID
		if corpID == "" {
			h.logger.Warn("WeCom encrypted mode is missing the CorpID config")
			c.String(http.StatusOK, "")
			return
		}

		// construct the full plaintext XML reply (format strictly per the WeCom documentation)
		plainResp := fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`, toUser, fromUser, time.Now().Unix(), content)

		encrypted, err := wecomEncrypt(h.config.Robots.Wecom.EncodingAESKey, plainResp, corpID)
		if err != nil {
			h.logger.Warn("failed to encrypt WeCom reply", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		// generate the signature using the timestamp/nonce from the request (WeCom requires the same timestamp and nonce as the request when replying)
		msgSignature := h.signWecomRequest(h.config.Robots.Wecom.Token, timestamp, nonce, encrypted)

		h.logger.Debug("WeCom sending encrypted reply",
			zap.String("Encrypt", encrypted[:50]+"..."),
			zap.String("MsgSignature", msgSignature),
			zap.String("TimeStamp", timestamp),
			zap.String("Nonce", nonce))

		// encrypted mode returns only the 4 core fields (per WeCom's official requirements)
		xmlResp := fmt.Sprintf(`<xml><Encrypt><![CDATA[%s]]></Encrypt><MsgSignature><![CDATA[%s]]></MsgSignature><TimeStamp><![CDATA[%s]]></TimeStamp><Nonce><![CDATA[%s]]></Nonce></xml>`, encrypted, msgSignature, timestamp, nonce)
		// also log the final response body so we can cross-check with the
		// network traffic or developer console
		h.logger.Debug("WeCom encrypted reply payload", zap.String("xml", xmlResp))
		// for additional confidence, decrypt the payload ourselves and log it
		if dec, err2 := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, encrypted); err2 == nil {
			h.logger.Debug("WeCom encrypted reply decryption check", zap.String("plain", string(dec)))
		} else {
			h.logger.Warn("WeCom encrypted reply decryption check failed", zap.Error(err2))
		}

		// write the response directly via c.Writer.Write, to avoid escaping issues with c.String
		c.Writer.WriteHeader(http.StatusOK)
		// use text/xml as that's what WeCom examples show
		c.Writer.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = c.Writer.Write([]byte(xmlResp))
		h.logger.Debug("WeCom encrypted reply sent")
		return
	}

	// plaintext mode
	h.logger.Debug("WeCom sending plaintext reply", zap.String("ToUserName", toUser), zap.String("FromUserName", fromUser), zap.String("Content", content[:50]+"..."))

	// manually construct the XML response (wrap every field in CDATA, and include AgentID)
	xmlResp := fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`, toUser, fromUser, time.Now().Unix(), content)

	// log the exact plaintext response for debugging
	h.logger.Debug("WeCom plaintext reply payload", zap.String("xml", xmlResp))

	// use text/xml as recommended by WeCom docs
	c.Header("Content-Type", "text/xml; charset=utf-8")
	c.String(http.StatusOK, xmlResp)
	h.logger.Debug("WeCom plaintext reply sent")
}

// —————— Test endpoints (login required; used to verify bot logic without a DingTalk/Feishu client) ——————

// CreateRobotBindingCode creates a short-lived, single-use secret for the
// currently authenticated RBAC user. Only its hash is persisted.
func (h *RobotHandler) CreateRobotBindingCode(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok || strings.TrimSpace(session.UserID) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized access"})
		return
	}
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate binding code"})
		return
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random)
	code := raw[:4] + "-" + raw[4:]
	expiresAt := time.Now().Add(robotBindingCodeTTL)
	if err := h.db.CreateRobotBindingCode(session.UserID, hashRobotBindingCode(code), expiresAt); err != nil {
		h.logger.Warn("failed to create robot binding code", zap.String("user_id", session.UserID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate binding code"})
		return
	}
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{Category: "auth", Action: "robot_binding_code_create", Result: "success", ResourceType: "user", ResourceID: session.UserID, Message: "generated a one-time robot binding code"})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"code": code, "expires_at": expiresAt.UTC().Format(time.RFC3339), "expires_in_seconds": int(robotBindingCodeTTL.Seconds()),
	})
}

func (h *RobotHandler) ListMyRobotBindings(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized access"})
		return
	}
	bindings, err := h.db.ListRobotUserBindings(session.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get robot bindings"})
		return
	}
	items := make([]gin.H, 0, len(bindings))
	for _, binding := range bindings {
		sum := sha256.Sum256([]byte(binding.ExternalUserID))
		items = append(items, gin.H{
			"id": binding.ID, "platform": binding.Platform, "external_user_hint": fmt.Sprintf("%x", sum[:4]),
			"enabled": binding.Enabled, "created_at": binding.CreatedAt, "updated_at": binding.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"bindings": items})
}

func (h *RobotHandler) DeleteMyRobotBinding(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized access"})
		return
	}
	if err := h.db.DeleteRobotUserBindingForUser(c.Param("id"), session.UserID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "binding does not exist"})
		return
	}
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{Category: "auth", Action: "robot_binding_revoke", Result: "success", ResourceType: "robot_binding", ResourceID: c.Param("id"), Message: "revoked robot platform account binding"})
	}
	c.Status(http.StatusNoContent)
}

// RobotTestRequest simulates a robot message request
type RobotTestRequest struct {
	Platform string `json:"platform"` // e.g. "dingtalk", "lark", "wecom"
	UserID   string `json:"user_id"`
	Text     string `json:"text"`
}

// HandleRobotTest is for local verification: POST JSON { "platform", "user_id", "text" }, returns { "reply": "..." }
func (h *RobotHandler) HandleRobotTest(c *gin.Context) {
	var req RobotTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the request body must be JSON containing platform, user_id, and text"})
		return
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = "test"
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "test_user"
	}
	reply := h.HandleMessage(platform, userID, req.Text)
	c.JSON(http.StatusOK, gin.H{"reply": reply})
}

// sendWecomMessageViaAPI proactively sends a message via the WeCom API (used to send the result after async processing)
func (h *RobotHandler) sendWecomMessageViaAPI(toUser, toParty, content string) {
	if !h.config.Robots.Wecom.Enabled {
		return
	}

	secret := h.config.Robots.Wecom.Secret
	corpID := h.config.Robots.Wecom.CorpID
	agentID := h.config.Robots.Wecom.AgentID

	if secret == "" || corpID == "" {
		h.logger.Warn("the WeCom proactive-send API is missing the secret or corpID config")
		return
	}

	// step 1: get the access_token
	tokenURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s", corpID, secret)
	resp, err := http.Get(tokenURL)
	if err != nil {
		h.logger.Warn("failed to get WeCom token", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		h.logger.Warn("failed to parse WeCom token response", zap.Error(err))
		return
	}
	if tokenResp.ErrCode != 0 {
		h.logger.Warn("error getting WeCom token", zap.String("errmsg", tokenResp.ErrMsg), zap.Int("errcode", tokenResp.ErrCode))
		return
	}

	// step 2: construct the send-message request
	msgReq := map[string]interface{}{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": agentID,
		"text": map[string]interface{}{
			"content": content,
		},
	}

	msgBody, err := json.Marshal(msgReq)
	if err != nil {
		h.logger.Warn("failed to serialize WeCom message", zap.Error(err))
		return
	}

	// step 3: send the message
	sendURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", tokenResp.AccessToken)
	msgResp, err := http.Post(sendURL, "application/json", bytes.NewReader(msgBody))
	if err != nil {
		h.logger.Warn("failed to proactively send WeCom message", zap.Error(err))
		return
	}
	defer msgResp.Body.Close()

	var sendResp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		InvalidUser string `json:"invaliduser"`
		MsgID       string `json:"msgid"`
	}
	if err := json.NewDecoder(msgResp.Body).Decode(&sendResp); err != nil {
		h.logger.Warn("failed to parse WeCom send response", zap.Error(err))
		return
	}

	if sendResp.ErrCode == 0 {
		h.logger.Debug("proactively sent WeCom message successfully", zap.String("msgid", sendResp.MsgID))
	} else {
		h.logger.Warn("failed to proactively send WeCom message", zap.String("errmsg", sendResp.ErrMsg), zap.Int("errcode", sendResp.ErrCode), zap.String("invaliduser", sendResp.InvalidUser))
	}
}

// —————— DingTalk ——————

// HandleDingtalkPOST handles the DingTalk event callback (streaming integration, etc.); currently a placeholder that returns 200
func (h *RobotHandler) HandleDingtalkPOST(c *gin.Context) {
	if !h.config.Robots.Dingtalk.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	// The DingTalk streaming/event callback format must be parsed per the official docs and replied to asynchronously; this just returns 200 for now
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// —————— Feishu ——————

// HandleLarkPOST handles the Feishu event callback; currently a placeholder that returns 200; verification requires returning the challenge
func (h *RobotHandler) HandleLarkPOST(c *gin.Context) {
	if !h.config.Robots.Lark.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	var body struct {
		Challenge string `json:"challenge"`
	}
	if err := c.ShouldBindJSON(&body); err == nil && body.Challenge != "" {
		c.JSON(http.StatusOK, gin.H{"challenge": body.Challenge})
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}
