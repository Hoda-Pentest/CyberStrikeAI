package c2

import "context"

type hitlRunCtxKey struct{}

// WithHITLRunContext attaches runCtx (usually the whole Agent / SSE request lifecycle) to the given ctx.
// The ctx an MCP tool handler receives may be a child context with a single-tool timeout, cancelled when the tool returns;
// dangerous-task HITL should use runCtx via HITLUserContext to wait for manual approval.
func WithHITLRunContext(ctx, runCtx context.Context) context.Context {
	if ctx == nil || runCtx == nil {
		return ctx
	}
	return context.WithValue(ctx, hitlRunCtxKey{}, runCtx)
}

// HITLUserContext returns the context used for C2 dangerous-task HITL waiting:
// if a longer-lived runCtx was injected via WithHITLRunContext it returns that, otherwise it returns ctx.
func HITLUserContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if v := ctx.Value(hitlRunCtxKey{}); v != nil {
		if run, ok := v.(context.Context); ok && run != nil {
			return run
		}
	}
	return ctx
}
