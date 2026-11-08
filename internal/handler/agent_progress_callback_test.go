package handler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/openai"

	"go.uber.org/zap"
)

// TestCreateProgressCallback_ConcurrentToolEvents is a regression test for issue #142: concurrent tool callbacks must not cause a concurrent map panic.
func TestCreateProgressCallback_ConcurrentToolEvents(t *testing.T) {
	logger := zap.NewNop()
	h := &AgentHandler{
		logger: logger,
		config: &config.Config{},
	}
	cb := h.createProgressCallback(context.Background(), nil, "conv-race-test", "", nil)

	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			toolCallID := fmt.Sprintf("tc-%d", i)
			cb("tool_call", "calling skill", map[string]interface{}{
				"toolCallId":   toolCallID,
				"toolName":     "skill",
				"argumentsObj": map[string]interface{}{"skill_name": "demo-skill"},
			})
		}()
		go func() {
			defer wg.Done()
			toolCallID := fmt.Sprintf("tc-%d", i)
			cb("tool_result", "skill done", map[string]interface{}{
				"toolCallId": toolCallID,
				"toolName":   "skill",
				"success":    true,
			})
		}()
	}
	wg.Wait()
}

// TestCreateProgressCallback_MirrorsWebStreamEvents verifies that after a page refresh, the task-events
// subscription must keep receiving subsequent events from the original Web SSE task, rather than only waiting for the final database result.
func TestCreateProgressCallback_MirrorsWebStreamEvents(t *testing.T) {
	bus := NewTaskEventBus()
	h := &AgentHandler{logger: zap.NewNop(), config: &config.Config{}, taskEventBus: bus}
	_, events := bus.Subscribe("conv-refresh-stream")
	primaryCalls := 0
	cb := h.createProgressCallback(
		context.Background(), nil, "conv-refresh-stream", "",
		func(eventType, message string, data interface{}) { primaryCalls++ },
	)

	cb("progress", "round 3", map[string]interface{}{"iteration": 3})
	if primaryCalls != 1 {
		t.Fatalf("expected primary SSE callback once, got %d", primaryCalls)
	}
	select {
	case payload := <-events:
		body := string(payload)
		if !strings.Contains(body, `"type":"progress"`) || !strings.Contains(body, `"conversationId":"conv-refresh-stream"`) {
			t.Fatalf("unexpected mirrored event: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("expected progress event mirrored to task event bus")
	}
}

func TestCreateProgressCallback_HidesInternalEinoDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.sqlite"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conv, err := db.CreateConversation("diag-hidden", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	asst, err := db.AddMessage(conv.ID, "assistant", "Processing...", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	bus := NewTaskEventBus()
	h := &AgentHandler{logger: zap.NewNop(), db: db, taskEventBus: bus}
	_, events := bus.Subscribe(conv.ID)
	primaryCalls := 0
	cb := h.createProgressCallback(
		context.Background(), nil, conv.ID, asst.ID,
		func(string, string, interface{}) { primaryCalls++ },
	)

	cb("model_output_rejected", "The model's tool call was incomplete or had unsafe arguments; execution was blocked and a rewrite was requested.", map[string]interface{}{
		"reason": "invalid_tool_arguments_json",
	})
	// NOTE: this literal must stay byte-for-byte identical to the Chinese
	// progress message emitted by internal/multiagent (out of this package's
	// translation scope) and matched by isInternalEinoDiagnosticProgress in
	// agent.go; encoded as \u escapes so the match still works without
	// literal Chinese characters in this file.
	cb("progress", "Eino TurnLoop \u5e38\u9a7b\u591a\u8f6e runtime \u5df2\u63a5\u7ba1\u672c\u8f6e\u4f1a\u8bdd\u3002", map[string]interface{}{
		"kind": "turn_loop_takeover",
	})

	if primaryCalls != 0 {
		t.Fatalf("primary SSE calls = %d, want hidden diagnostics", primaryCalls)
	}
	select {
	case payload := <-events:
		t.Fatalf("unexpected mirrored diagnostic event: %s", string(payload))
	default:
	}
	details, err := db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails: %v", err)
	}
	if len(details) != 0 {
		t.Fatalf("process details = %+v, want no diagnostics persisted", details)
	}
}

func TestCreateProgressCallback_PersistsRunningResponseBeforeDone(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.sqlite"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conv, err := db.CreateConversation("refresh-running", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	asst, err := db.AddMessage(conv.ID, "assistant", "Processing...", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h := &AgentHandler{logger: zap.NewNop(), db: db}
	cb := h.createProgressCallback(context.Background(), nil, conv.ID, asst.ID, nil)
	meta := map[string]interface{}{
		"streamId":      "response-refresh-1",
		"einoAgent":     "cyberstrike-eino-single",
		"orchestration": "eino_single",
	}
	cb("response_start", "", meta)
	cb("response_delta", "the first part generated before the refresh", openai.WithSSEAccumulated(meta, "the first part generated before the refresh"))

	details, err := db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails: %v", err)
	}
	if len(details) != 1 || details[0].EventType != "planning" || details[0].Message != "the first part generated before the refresh" {
		t.Fatalf("expected one running planning snapshot, got %+v", details)
	}

	longer := "the first part generated before the refresh" + strings.Repeat("continuing iteration", 300)
	cb("response_delta", "continuing iteration", openai.WithSSEAccumulated(meta, longer))
	details, err = db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails after update: %v", err)
	}
	if len(details) != 1 || details[0].Message != longer {
		t.Fatalf("running snapshot should update in-place, rows=%d len=%d", len(details), len(details[0].Message))
	}
}

// TestCreateProgressCallback_FlushesReasoningOnDone verifies that streaming reasoning aggregation is persisted to the database on done/response, and can be replayed after a refresh.
func TestCreateProgressCallback_FlushesReasoningOnDone(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.sqlite"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	conv, err := db.CreateConversation("test", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	asst, err := db.AddMessage(conv.ID, "assistant", "Processing...", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h := &AgentHandler{logger: zap.NewNop(), db: db}
	cb := h.createProgressCallback(context.Background(), nil, conv.ID, asst.ID, nil)

	streamID := "eino-reasoning-test-1"
	cb("reasoning_chain_stream_start", " ", map[string]interface{}{
		"streamId": streamID,
		"source":   "eino",
	})
	cb("reasoning_chain_stream_delta", "step one", openai.WithSSEAccumulated(map[string]interface{}{
		"streamId": streamID,
	}, "step one"))
	cb("done", "", map[string]interface{}{"conversationId": conv.ID})

	details, err := db.GetProcessDetails(asst.ID)
	if err != nil {
		t.Fatalf("GetProcessDetails: %v", err)
	}
	found := false
	for _, d := range details {
		if d.EventType == "reasoning_chain" && d.Message == "step one" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected reasoning_chain persisted on done, got %+v", details)
	}
}

func TestEnrichProgressEventData(t *testing.T) {
	t.Run("fills ids", func(t *testing.T) {
		out := enrichProgressEventData(map[string]interface{}{"source": "eino"}, "conv-1", "msg-1")
		m, ok := out.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map, got %T", out)
		}
		if m["conversationId"] != "conv-1" || m["messageId"] != "msg-1" {
			t.Fatalf("unexpected enrichment: %+v", m)
		}
	})
	t.Run("preserves existing ids", func(t *testing.T) {
		out := enrichProgressEventData(map[string]interface{}{
			"conversationId": "keep-conv",
			"messageId":      "keep-msg",
		}, "conv-1", "msg-1")
		m := out.(map[string]interface{})
		if m["conversationId"] != "keep-conv" || m["messageId"] != "keep-msg" {
			t.Fatalf("should not overwrite existing ids: %+v", m)
		}
	})
}
