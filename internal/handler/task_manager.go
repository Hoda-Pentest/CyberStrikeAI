package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/multiagent"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/runlease"
	"cyberstrike-ai/internal/security"
)

// ErrTaskCancelled the error for a user-cancelled task
var ErrTaskCancelled = errors.New("agent task cancelled by user")

// ErrTaskAlreadyRunning the session already has a task running
var ErrTaskAlreadyRunning = errors.New("agent task already running for conversation")

// shouldPersistAgentAgentTraceAfterRunError: whether to still write last_react_* for the next loadHistoryFromAgentTrace when an Agent-related Run returns unsuccessfully.
// Current policy: whether the run ends normally, abnormally, or is stopped by the user, keep the last usable trace whenever possible,
// so continuing in the same session can resume from the original context instead of falling back to message-text-only history.
func shouldPersistAgentAgentTraceAfterRunError(baseCtx context.Context) bool {
	return true
}

// AgentTask describes a running Agent task
type AgentTask struct {
	RunID            string `json:"runId"`
	CleanupError     string `json:"cleanupError,omitempty"`
	processes        *security.ProcessScope
	workers          *runlease.Scope
	IsolationBackend string `json:"isolationBackend,omitempty"`
	finishing        chan struct{}
	stopping         chan struct{}
	cancelRequested  bool
	finalStatus      string
	ConversationID   string    `json:"conversationId"`
	Title            string    `json:"title,omitempty"`
	Message          string    `json:"message,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	Status           string    `json:"status"`
	CancellingAt     time.Time `json:"-"` // the time of entering the cancelling state, used to clean up long-stuck tasks

	// ActiveMCPExecutionID the executionId of the MCP tool currently executing (memory only, for "interrupt and continue" = only cut off the current tool)
	ActiveMCPExecutionID string `json:"-"`

	// InterruptContinueNote the supplementary note the user fills in the dialog for "interrupt and continue" when there is no MCP (written before Cancel, read and cleared on the resumed round)
	InterruptContinueNote string `json:"-"`

	// activeAgentExecuteCancel is the cancel function of the in-progress Agent filesystem execute (parallel to MCP tools, used for interrupt-and-continue)
	activeAgentExecuteCancel context.CancelFunc
	// activeAgentExecuteAbortNote is the user note written by AbortActiveAgentExecute, merged into the tool result when execute finishes
	activeAgentExecuteAbortNote string

	// hitlCognition the context read by the HITL/audit Agent during this run (the user's original words + reasoning, no conversation history)
	hitlCognition *hitlCognitionState

	// agentRuntimeCancel wraps the current Pi native AgentCancelFunc; cancelling a task triggers it first, then falls back to context.
	agentRuntimeCancel        func(error) bool
	agentRuntimeCancelVersion uint64

	// agentTurnLoopInterrupt is the current Agent TurnLoop user-addition push hook; on interrupt-and-continue the addition is preferably enqueued as a new turn item.
	agentTurnLoopInterrupt        func(string) bool
	agentTurnLoopInterruptVersion uint64

	cancel func(error)
}

// RegisterRunningTool implements mcp.ToolRunRegistry: registers the current executionId of this session when the tool starts.
func (m *AgentTaskManager) RegisterRunningTool(conversationID, executionID string) {
	conversationID = strings.TrimSpace(conversationID)
	executionID = strings.TrimSpace(executionID)
	if conversationID == "" || executionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.ActiveMCPExecutionID = executionID
	}
}

// UnregisterRunningTool clears the registration when the tool ends (only when the id still matches, to avoid cross-wiring under concurrency).
func (m *AgentTaskManager) UnregisterRunningTool(conversationID, executionID string) {
	conversationID = strings.TrimSpace(conversationID)
	executionID = strings.TrimSpace(executionID)
	if conversationID == "" || executionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		if t.ActiveMCPExecutionID == executionID {
			t.ActiveMCPExecutionID = ""
		}
	}
}

// RegisterActiveAgentExecute registers the in-progress Agent filesystem execute (only one per session at a time).
func (m *AgentTaskManager) RegisterActiveAgentExecute(conversationID string, cancel context.CancelFunc) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || cancel == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.activeAgentExecuteCancel = cancel
		t.activeAgentExecuteAbortNote = ""
	}
}

// UnregisterActiveAgentExecute clears the registration after execute ends normally or is cancelled.
func (m *AgentTaskManager) UnregisterActiveAgentExecute(conversationID string) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.activeAgentExecuteCancel = nil
		t.activeAgentExecuteAbortNote = ""
	}
}

// ConversationIDForActiveMCPExecution looks up the session ID by the currently registered tool executionId (for the MCP monitoring page to terminate by executionId).
func (m *AgentTaskManager) ConversationIDForActiveMCPExecution(executionID string) string {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for convID, t := range m.tasks {
		if t != nil && t.ActiveMCPExecutionID == executionID {
			return convID
		}
	}
	return ""
}

// ConversationIDForActiveAgentExecute returns the session ID of the only in-progress Agent execute; returns empty when multiple sessions run in parallel.
func (m *AgentTaskManager) ConversationIDForActiveAgentExecute() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var found string
	count := 0
	for convID, t := range m.tasks {
		if t != nil && t.activeAgentExecuteCancel != nil {
			found = convID
			count++
		}
	}
	if count == 1 {
		return found, true
	}
	return "", false
}

// AbortActiveAgentExecute terminates the current Agent execute and stores the user note (consistent with MCP tool termination).
func (m *AgentTaskManager) AbortActiveAgentExecute(conversationID, note string) bool {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return false
	}
	m.mu.Lock()
	t, ok := m.tasks[conversationID]
	if !ok || t == nil || t.activeAgentExecuteCancel == nil {
		m.mu.Unlock()
		return false
	}
	t.activeAgentExecuteAbortNote = strings.TrimSpace(note)
	cancel := t.activeAgentExecuteCancel
	m.mu.Unlock()
	cancel()
	return true
}

// TakeAgentExecuteAbortNote reads and clears the execute termination note (called once when execute finishes).
func (m *AgentTaskManager) TakeAgentExecuteAbortNote(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		n := t.activeAgentExecuteAbortNote
		t.activeAgentExecuteAbortNote = ""
		return n
	}
	return ""
}

// SetInterruptContinueNote writes the user's supplementary note before issuing an ErrInterruptContinue cancel (memory only).
func (m *AgentTaskManager) SetInterruptContinueNote(conversationID, note string) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.InterruptContinueNote = note
	}
}

// TakeInterruptContinueNote reads and clears the supplementary note (called once when the resumed round starts).
func (m *AgentTaskManager) TakeInterruptContinueNote(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		n := t.InterruptContinueNote
		t.InterruptContinueNote = ""
		return n
	}
	return ""
}

// BindTaskCancel replaces the context-bound cancel function within the same running task (used to swap in a new baseCtx when continuing after an interrupt).
func (m *AgentTaskManager) BindTaskCancel(conversationID string, cancel context.CancelCauseFunc) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || cancel == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		t.cancel = func(err error) {
			cancel(err)
		}
	}
}

// BindAgentRuntimeCancel registers the Agent native cancel hook of the current run segment.
func (m *AgentTaskManager) BindAgentRuntimeCancel(conversationID string, cancel func(error) bool) func() {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || cancel == nil {
		return func() {}
	}
	m.mu.Lock()
	t, ok := m.tasks[conversationID]
	if !ok || t == nil {
		m.mu.Unlock()
		return func() {}
	}
	t.agentRuntimeCancelVersion++
	version := t.agentRuntimeCancelVersion
	t.agentRuntimeCancel = cancel
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if cur, exists := m.tasks[conversationID]; exists && cur != nil && cur.agentRuntimeCancelVersion == version {
			cur.agentRuntimeCancel = nil
		}
	}
}

// BindAgentTurnLoopInterrupt registers the Agent TurnLoop user-addition enqueue hook of the current running task.
func (m *AgentTaskManager) BindAgentTurnLoopInterrupt(conversationID string, push func(string) bool) func() {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || push == nil {
		return func() {}
	}
	m.mu.Lock()
	t, ok := m.tasks[conversationID]
	if !ok || t == nil {
		m.mu.Unlock()
		return func() {}
	}
	t.agentTurnLoopInterruptVersion++
	version := t.agentTurnLoopInterruptVersion
	t.agentTurnLoopInterrupt = push
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if cur, exists := m.tasks[conversationID]; exists && cur != nil && cur.agentTurnLoopInterruptVersion == version {
			cur.agentTurnLoopInterrupt = nil
		}
	}
}

// ActiveMCPExecutionID returns the in-progress tool executionId of the current session, or an empty string if none.
func (m *AgentTaskManager) ActiveMCPExecutionID(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.tasks[conversationID]; ok && t != nil {
		return strings.TrimSpace(t.ActiveMCPExecutionID)
	}
	return ""
}

// CompletedTask a completed task (used for history)
type CompletedTask struct {
	CleanupError     string    `json:"cleanupError,omitempty"`
	IsolationBackend string    `json:"isolationBackend,omitempty"`
	RunID            string    `json:"runId"`
	ConversationID   string    `json:"conversationId"`
	Title            string    `json:"title,omitempty"`
	Message          string    `json:"message,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	CompletedAt      time.Time `json:"completedAt"`
	Status           string    `json:"status"`
}

// AgentTaskManager manages running Agent tasks
type AgentTaskManager struct {
	mu               sync.RWMutex
	tasks            map[string]*AgentTask
	completedTasks   []*CompletedTask // history of recently completed tasks
	maxHistorySize   int              // maximum number of history records
	historyRetention time.Duration    // how long to retain history records
	eventBus         *TaskEventBus    // optional: closes the mirrored SSE subscription when the task ends
	// toolCanceler terminates the MCP tools still running in a session when the user stops the whole-round task or the session ends (not "interrupt and continue").
	toolCanceler func(conversationID string)
	shuttingDown bool
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

const (
	// cancellingStuckThreshold if "cancelling" lasts longer than this, forcibly remove it from the running list. A normal cancel returns within the current step,
	// if it exceeds this, treat it as stuck and release the session as soon as possible. The common practice is to release within 30-60s.
	cancellingStuckThreshold = 45 * time.Second
	// cancellingStuckThresholdLegacy the fallback duration judged by StartedAt when CancellingAt is not recorded
	cancellingStuckThresholdLegacy = 2 * time.Minute
	cleanupInterval                = 15 * time.Second // works with the threshold above, removed within about 60s at most
)

// NewAgentTaskManager creates a task manager
func NewAgentTaskManager() *AgentTaskManager {
	m := &AgentTaskManager{
		tasks:            make(map[string]*AgentTask),
		shutdown:         make(chan struct{}),
		completedTasks:   make([]*CompletedTask, 0),
		maxHistorySize:   50,             // keep at most 50 history records
		historyRetention: 24 * time.Hour, // retain for 24 hours
	}
	go m.runStuckCancellingCleanup()
	return m
}

// SetTaskEventBus sets the task event bus (shares the same instance as AgentHandler).
func (m *AgentTaskManager) SetTaskEventBus(b *TaskEventBus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventBus = b
}

// SetToolCanceler sets the callback that terminates MCP tools still running when a whole round stops a task/the session ends (injected by AgentHandler).
func (m *AgentTaskManager) SetToolCanceler(fn func(conversationID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCanceler = fn
}

// GetTask returns the running task (nil if none).
func (m *AgentTaskManager) GetTask(conversationID string) *AgentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tasks[conversationID]
}

// GetTaskSnapshot returns a read-only copy of the running task for status display, avoiding reading mutable task fields outside the lock.
func (m *AgentTaskManager) GetTaskSnapshot(conversationID string) *AgentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task := m.tasks[conversationID]
	if task == nil {
		return nil
	}
	snapshot := *task
	snapshot.IsolationBackend = task.processes.IsolationBackend()
	return &snapshot
}

// runStuckCancellingCleanup periodically forces tasks stuck in "cancelling" for a long time to finish, to avoid being stuck and unable to send new messages
func (m *AgentTaskManager) runStuckCancellingCleanup() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.shutdown:
			return
		case <-ticker.C:
			m.cleanupStuckCancelling()
		}
	}
}

func (m *AgentTaskManager) cleanupStuckCancelling() {
	m.mu.Lock()
	type pendingFinish struct{ id, runID, status string }
	var toFinish []pendingFinish
	now := time.Now()
	for id, task := range m.tasks {
		if task.Status == "cleanup_failed" {
			toFinish = append(toFinish, pendingFinish{id, task.RunID, task.finalStatus})
			continue
		}
		if task.Status != "cancelling" {
			continue
		}
		var elapsed time.Duration
		if !task.CancellingAt.IsZero() {
			elapsed = now.Sub(task.CancellingAt)
			if elapsed < cancellingStuckThreshold {
				continue
			}
		} else {
			elapsed = now.Sub(task.StartedAt)
			if elapsed < cancellingStuckThresholdLegacy {
				continue
			}
		}
		toFinish = append(toFinish, pendingFinish{id, task.RunID, "cancelled"})
	}
	m.mu.Unlock()
	for _, pending := range toFinish {
		_ = m.FinishTaskRun(pending.id, pending.runID, pending.status)
	}
}

// StartTask registers and starts a new task
func (m *AgentTaskManager) StartTask(conversationID, message string, cancel context.CancelCauseFunc) (*AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shuttingDown {
		return nil, errors.New("task manager is shutting down")
	}
	if _, exists := m.tasks[conversationID]; exists {
		return nil, ErrTaskAlreadyRunning
	}

	scope := security.NewProcessScope()
	task := &AgentTask{
		RunID: scope.ID, processes: scope, workers: runlease.New(),
		ConversationID: conversationID,
		Message:        message,
		StartedAt:      time.Now(),
		Status:         "running",
		cancel: func(err error) {
			if cancel != nil {
				cancel(err)
			}
		},
	}

	m.tasks[conversationID] = task
	task.hitlCognition = &hitlCognitionState{UserMessage: strings.TrimSpace(message)}
	return task, nil
}

// CancelTask cancels the task of the specified session. If the task is already being cancelled, still returns (true, nil) so the API is idempotent and the frontend does not error.
func (m *AgentTaskManager) CancelTask(conversationID string, cause error) (bool, error) {
	m.mu.Lock()
	task, exists := m.tasks[conversationID]
	if !exists {
		m.mu.Unlock()
		return false, nil
	}

	// if it is already in the cancellation flow, treat it as success (idempotent), to avoid the frontend showing "task not found" on repeated clicks
	if task.Status == "cancelling" || task.finishing != nil {
		m.mu.Unlock()
		return true, nil
	}

	if cause == nil || errors.Is(cause, ErrTaskCancelled) {
		task.cancelRequested = true
		task.finalStatus = "cancelled"
	}

	// ErrInterruptContinue: only cuts off the current reasoning step; the handler then resumes, without entering a long-lasting "cancelling" state.
	if cause != nil && errors.Is(cause, multiagent.ErrInterruptContinue) {
		task.Status = "running"
	} else {
		task.Status = "cancelling"
		task.CancellingAt = time.Now()
	}
	if cause != nil && errors.Is(cause, ErrTaskCancelled) {
		task.InterruptContinueNote = ""
	}
	cancel := task.cancel
	if cause == nil {
		cause = ErrTaskCancelled
	}
	interruptPush := task.agentTurnLoopInterrupt
	interruptNote := task.InterruptContinueNote
	runtimeCancel := task.agentRuntimeCancel
	activeExecuteCancel := task.activeAgentExecuteCancel
	if !errors.Is(cause, multiagent.ErrInterruptContinue) {
		task.processes.Seal()
		task.workers.Seal()
		task.stopping = make(chan struct{})
		defer close(task.stopping)
	}
	var toolCanceler func(string)
	if errors.Is(cause, ErrTaskCancelled) {
		toolCanceler = m.toolCanceler
	}
	m.mu.Unlock()

	if errors.Is(cause, multiagent.ErrInterruptContinue) && interruptPush != nil && interruptPush(interruptNote) {
		m.mu.Lock()
		if cur, exists := m.tasks[conversationID]; exists && cur != nil {
			cur.InterruptContinueNote = ""
		}
		m.mu.Unlock()
		return true, nil
	}

	runtimeHandled := false
	if runtimeCancel != nil {
		runtimeHandled = runtimeCancel(cause)
	}
	// "Hard stop" must also cancel the host context: even if the native Agent Cancel has been accepted,
	// it may also only return at a safe point or report a timeout, which must not keep the whole task alive.
	// interrupt and continue still keeps the original semantics: when the native cancel has been handled, the runtime is responsible for resuming.
	if cancel != nil && (!runtimeHandled || errors.Is(cause, ErrTaskCancelled)) {
		cancel(cause)
	}
	if toolCanceler != nil {
		toolCanceler(conversationID)
	}
	if !errors.Is(cause, multiagent.ErrInterruptContinue) {
		task.workers.Cancel()
		if activeExecuteCancel != nil {
			activeExecuteCancel()
		}
		return true, task.processes.Close()
	}
	return true, nil
}

// UpdateTaskStatus updates the task status but does not delete the task (used to update status before sending an event)
func (m *AgentTaskManager) UpdateTaskStatus(conversationID string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[conversationID]
	if !exists {
		return
	}

	if task.finishing != nil || task.Status == "cleanup_failed" {
		return
	}
	if task.cancelRequested {
		status = "cancelled"
	}
	switch status {
	case "completed", "cancelled", "failed", "timeout":
		task.finalStatus = status
		task.Status = "cleaning"
		task.processes.Seal()
		task.workers.Seal()
	default:
		if status != "" {
			task.Status = status
		}
	}
}

// BindProcessScope snapshots ownership once at task start. Continuations must
// derive from this context, never resolve ownership again using conversation ID.
func (m *AgentTaskManager) BindProcessScope(ctx context.Context, conversationID, runID string) context.Context {
	// put the conversation ID into ctx: the %session_id% placeholder in custom request headers is replaced accordingly,
	// ensure the x-opencode-session and other headers sent on each conversation correspond to the current session.
	ctx = openai.WithSessionID(ctx, conversationID)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if task := m.tasks[conversationID]; task != nil && task.RunID == runID {
		return runlease.WithScope(security.WithProcessScope(ctx, task.processes), task.workers)
	}
	// Fail closed if a task disappeared before its execution context was bound.
	scope := security.NewProcessScope()
	scope.Seal()
	workers := runlease.New()
	workers.Seal()
	return runlease.WithScope(security.WithProcessScope(ctx, scope), workers)
}

// FinishTask is retained for callers that operate on the current task. Owners
// use FinishTaskRun, so a delayed defer cannot finish a newer conversation run.
func (m *AgentTaskManager) FinishTask(conversationID string, finalStatus string) {
	m.mu.RLock()
	task := m.tasks[conversationID]
	m.mu.RUnlock()
	if task != nil {
		_ = m.FinishTaskRun(conversationID, task.RunID, finalStatus)
	}
}

func (m *AgentTaskManager) FinishTaskRun(conversationID, runID, finalStatus string) error {
	m.mu.Lock()
	task := m.tasks[conversationID]
	if task == nil || task.RunID != runID {
		m.mu.Unlock()
		return nil
	}
	if task.stopping != nil {
		select {
		case <-task.stopping:
		default:
			stopping := task.stopping
			m.mu.Unlock()
			<-stopping
			return m.FinishTaskRun(conversationID, runID, finalStatus)
		}
	}
	if task.finishing != nil {
		done := task.finishing
		m.mu.Unlock()
		<-done
		m.mu.RLock()
		cleanupError := task.CleanupError
		m.mu.RUnlock()
		if cleanupError != "" {
			return errors.New(cleanupError)
		}
		return nil
	}
	if task.cancelRequested {
		finalStatus = "cancelled"
	}
	done := make(chan struct{})
	task.finishing = done
	task.finalStatus = finalStatus
	task.Status = "cleaning"
	task.processes.Seal()
	task.workers.Seal()
	toolCanceler := m.toolCanceler
	activeCancel := task.activeAgentExecuteCancel
	cancel := task.cancel
	bus := m.eventBus
	m.mu.Unlock()

	// Keep the conversation occupied throughout cleanup, including callbacks.
	if cancel != nil {
		cancel(nil)
	}
	if toolCanceler != nil {
		toolCanceler(conversationID)
	}
	if activeCancel != nil {
		activeCancel()
	}
	task.workers.Cancel()
	processErr := task.processes.Close()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	workerErr := task.workers.Wait(waitCtx)
	waitCancel()
	cleanupErr := errors.Join(processErr, workerErr)
	if processErr != nil && errors.Is(workerErr, runlease.ErrUnconfirmed) {
		// A simultaneous local failure must not be labelled as local completion.
		cleanupErr = fmt.Errorf("local cleanup: %w; remote state: %v", processErr, workerErr)
	}
	// The local worker has returned, but remote notification cancellation is
	// not an acknowledgement. Preserve an actionable history/tool status.
	unconfirmed := processErr == nil && errors.Is(workerErr, runlease.ErrUnconfirmed)
	if unconfirmed {
		finalStatus = "cleanup_unconfirmed"
	}
	cleanupMessage := ""
	if cleanupErr != nil {
		cleanupMessage = cleanupErr.Error()
	}
	if (cleanupErr == nil || unconfirmed) && bus != nil {
		// Subscribers must receive completion only after local processes are reaped.
		payload, _ := json.Marshal(StreamEvent{Type: "done", Data: map[string]interface{}{"conversationId": conversationID, "runId": runID, "status": finalStatus, "cleanupError": cleanupMessage}})
		bus.Publish(conversationID, append(append([]byte("data: "), payload...), '\n', '\n'))
		bus.CloseConversation(conversationID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	defer close(done)
	if cleanupErr != nil && !unconfirmed {
		task.Status = "cleanup_failed"
		task.CleanupError = cleanupErr.Error()
		task.finishing = nil
		return cleanupErr
	}
	task.CleanupError = ""
	if unconfirmed {
		task.CleanupError = cleanupErr.Error()
	}
	task.Status = finalStatus
	m.completedTasks = append(m.completedTasks, &CompletedTask{
		RunID: task.RunID, CleanupError: task.CleanupError, IsolationBackend: task.processes.IsolationBackend(), ConversationID: task.ConversationID, Message: task.Message,
		StartedAt: task.StartedAt, CompletedAt: time.Now(), Status: finalStatus,
	})
	m.cleanupHistory()
	delete(m.tasks, conversationID)
	return cleanupErr
}

// Shutdown rejects new tasks before cancelling and reaping existing task jobs.
func (m *AgentTaskManager) Shutdown() {
	m.mu.Lock()
	m.shuttingDown = true
	m.shutdownOnce.Do(func() { close(m.shutdown) })
	tasks := make([]*AgentTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
		task.processes.Seal()
		task.workers.Seal()
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		go func(task *AgentTask) {
			defer wg.Done()
			_, _ = m.CancelTask(task.ConversationID, ErrTaskCancelled)
			_ = m.FinishTaskRun(task.ConversationID, task.RunID, "cancelled")
		}(task)
	}
	wg.Wait()
}

// cleanupHistory cleans up expired history records
func (m *AgentTaskManager) cleanupHistory() {
	now := time.Now()
	cutoffTime := now.Add(-m.historyRetention)

	// filter out expired records
	validTasks := make([]*CompletedTask, 0, len(m.completedTasks))
	for _, task := range m.completedTasks {
		if task.CompletedAt.After(cutoffTime) {
			validTasks = append(validTasks, task)
		}
	}

	// if it still exceeds the maximum count, keep only the most recent ones
	if len(validTasks) > m.maxHistorySize {
		// sort by completion time, keeping the newest
		// since it is appended, the newest is at the end, so take the last N directly
		start := len(validTasks) - m.maxHistorySize
		validTasks = validTasks[start:]
	}

	m.completedTasks = validTasks
}

// GetActiveTasks returns all running tasks
func (m *AgentTaskManager) GetActiveTasks() []*AgentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*AgentTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		result = append(result, &AgentTask{
			RunID: task.RunID, CleanupError: task.CleanupError, IsolationBackend: task.processes.IsolationBackend(),
			ConversationID: task.ConversationID,
			Message:        task.Message,
			StartedAt:      task.StartedAt,
			Status:         task.Status,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].ConversationID < result[j].ConversationID
		}
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result
}

// GetCompletedTasks returns the recent completed-task history
func (m *AgentTaskManager) GetCompletedTasks() []*CompletedTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// clean up expired records (read lock only, does not affect other operations)
	// note: cleanupHistory cannot be called directly here, because a write lock is needed
	// so filter out expired records when returning
	now := time.Now()
	cutoffTime := now.Add(-m.historyRetention)

	result := make([]*CompletedTask, 0, len(m.completedTasks))
	for _, task := range m.completedTasks {
		if task.CompletedAt.After(cutoffTime) {
			result = append(result, task)
		}
	}

	// sort by completion time descending (newest first)
	// since it is appended, the newest is at the end, so it needs to be reversed
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	// limit the number of returned results
	if len(result) > m.maxHistorySize {
		result = result[:m.maxHistorySize]
	}

	return result
}
