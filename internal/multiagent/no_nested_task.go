package multiagent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// noNestedTaskMiddleware forbids calling task again while already in a task(sub-agent) execution chain,
// avoiding infinite delegation/recursion from a sub-agent delegating to another sub-agent.
//
// Nesting detection is implemented by setting a temporary marker in ctx: the outer task call marks ctx first,
// and a task call inside the sub-agent hits the marker and is rejected.
type noNestedTaskMiddleware struct {
	adk.BaseChatModelAgentMiddleware
}

type nestedTaskCtxKey struct{}

func newNoNestedTaskMiddleware() adk.ChatModelAgentMiddleware {
	return &noNestedTaskMiddleware{}
}

type noNestedAgenticTaskMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
}

func newNoNestedAgenticTaskMiddleware() adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &noNestedAgenticTaskMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
	}
}

func (m *noNestedTaskMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return wrapNoNestedTaskCall(ctx, endpoint, tCtx)
}

func (m *noNestedAgenticTaskMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return wrapNoNestedTaskCall(ctx, endpoint, tCtx)
}

func wrapNoNestedTaskCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx == nil || strings.TrimSpace(tCtx.Name) == "" {
		return endpoint, nil
	}
	// Deep's built-in task tool name is fixed as "task"; for compatibility with possible case/whitespace, only a case-insensitive match is done.
	if !strings.EqualFold(strings.TrimSpace(tCtx.Name), "task") {
		return endpoint, nil
	}

	// Already in a task execution chain: refuse further delegation and error out so the upper layer terminates quickly.
	if ctx != nil {
		if v, ok := ctx.Value(nestedTaskCtxKey{}).(bool); ok && v {
			return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
				// Important: return a tool result text (not an error) to avoid hard-stopping the whole multi-agent run.
				// The nested task is still prevented from spawning another sub-agent, so recursion is avoided.
				_ = argumentsInJSON
				_ = opts
				return "Nested task delegation is forbidden (already inside a sub-agent delegation chain) to avoid infinite delegation. Please continue the work using the current agent's tools.", nil
			}, nil
		}
	}

	// Mark the current task call chain, to ensure a repeated task call inside the sub-agent can detect the nesting.
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		ctx2 := ctx
		if ctx2 == nil {
			ctx2 = context.Background()
		}
		ctx2 = context.WithValue(ctx2, nestedTaskCtxKey{}, true)
		return endpoint(ctx2, argumentsInJSON, opts...)
	}, nil
}
