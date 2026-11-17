package mcp

import (
	"context"
	"strings"
)

// ToolRunRegistry registers the current executionId when a tool starts/ends, so the conversation page's "terminate the current tool only" and the monitoring page share cancel logic.
type ToolRunRegistry interface {
	RegisterRunningTool(conversationID, executionID string)
	UnregisterRunningTool(conversationID, executionID string)
}

// AgentExecuteRunRegistry registers in-progress Agent filesystem executes so "interrupt and continue" can terminate long commands such as amass.
type AgentExecuteRunRegistry interface {
	RegisterActiveAgentExecute(conversationID string, cancel context.CancelFunc)
	UnregisterActiveAgentExecute(conversationID string)
	AbortActiveAgentExecute(conversationID, note string) bool
	TakeAgentExecuteAbortNote(conversationID string) string
}

type toolRunRegistryCtxKey struct{}
type agentExecuteRunRegistryCtxKey struct{}
type mcpConversationIDCtxKey struct{}
type mcpExecutionIDCtxKey struct{}
type mcpProjectIDCtxKey struct{}

// WithToolRunRegistry injects the registry into ctx (Agent / native Agent task ctx).
func WithToolRunRegistry(ctx context.Context, reg ToolRunRegistry) context.Context {
	if ctx == nil || reg == nil {
		return ctx
	}
	return context.WithValue(ctx, toolRunRegistryCtxKey{}, reg)
}

// ToolRunRegistryFromContext retrieves the registry (nil if none).
func ToolRunRegistryFromContext(ctx context.Context) ToolRunRegistry {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(toolRunRegistryCtxKey{}).(ToolRunRegistry)
	return v
}

// WithAgentExecuteRunRegistry injects the Agent execute cancel registry into ctx.
func WithAgentExecuteRunRegistry(ctx context.Context, reg AgentExecuteRunRegistry) context.Context {
	if ctx == nil || reg == nil {
		return ctx
	}
	return context.WithValue(ctx, agentExecuteRunRegistryCtxKey{}, reg)
}

// AgentExecuteRunRegistryFromContext retrieves the Agent execute registry (nil if absent).
func AgentExecuteRunRegistryFromContext(ctx context.Context) AgentExecuteRunRegistry {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(agentExecuteRunRegistryCtxKey{}).(AgentExecuteRunRegistry)
	return v
}

// WithMCPConversationID injects the conversation ID into ctx, for correlation with the executionId inside CallTool.
func WithMCPConversationID(ctx context.Context, conversationID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, mcpConversationIDCtxKey{}, id)
}

// MCPConversationIDFromContext reads the conversation ID.
func MCPConversationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(mcpConversationIDCtxKey{}).(string)
	return v
}

// WithMCPExecutionID injects the current tool's executionId into ctx, to align the filename for oversized output spilled to disk.
func WithMCPExecutionID(ctx context.Context, executionID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(executionID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, mcpExecutionIDCtxKey{}, id)
}

// MCPExecutionIDFromContext reads the current tool's executionId.
func MCPExecutionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(mcpExecutionIDCtxKey{}).(string)
	return v
}

// WithMCPProjectID injects the project ID into ctx, to align the reduction/trunc spill path with project isolation.
func WithMCPProjectID(ctx context.Context, projectID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(projectID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, mcpProjectIDCtxKey{}, id)
}

// MCPProjectIDFromContext reads the project ID.
func MCPProjectIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(mcpProjectIDCtxKey{}).(string)
	return v
}

func notifyToolRunBegin(ctx context.Context, executionID string) {
	reg := ToolRunRegistryFromContext(ctx)
	if reg == nil {
		return
	}
	conv := MCPConversationIDFromContext(ctx)
	if conv == "" || strings.TrimSpace(executionID) == "" {
		return
	}
	reg.RegisterRunningTool(conv, executionID)
}

func notifyToolRunEnd(ctx context.Context, executionID string) {
	reg := ToolRunRegistryFromContext(ctx)
	if reg == nil {
		return
	}
	conv := MCPConversationIDFromContext(ctx)
	if conv == "" || strings.TrimSpace(executionID) == "" {
		return
	}
	reg.UnregisterRunningTool(conv, executionID)
}
