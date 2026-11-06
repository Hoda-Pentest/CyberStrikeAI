package openai

import (
	"context"
	"net/http"
	"strings"

	"cyberstrike-ai/internal/config"
)

// sessionHeaderPlaceholder is the placeholder, inside a custom request header value,
// that stands for "the current conversation ID".
// Example of how it is written (config file or the settings page):
//
//	headers:
//	  x-opencode-session: %session_id%
//
// It is replaced with the current conversation's conversation id when actually sent.
// Note: do not use {{...}} syntax; index.html is a Go template and would be parsed
// as a template function.
const sessionHeaderPlaceholder = "%session_id%"

type sessionIDContextKey struct{}

// WithSessionID injects the current conversation ID into ctx. The handler injects it
// once when a task starts, and every subsequent HTTP request made by the Eino
// ChatModel and the built-in Client can read it from the request context.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		return nil
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDContextKey{}, id)
}

// SessionIDFromContext reads the conversation ID from ctx, returning an empty string if none was injected.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(sessionIDContextKey{}).(string)
	return v
}

// renderHeaderValue replaces %session_id% in the value with the current conversation
// ID.
// A second return value of false means this header depends on the session ID but it
// could not be found in the context, so it should be skipped rather than sent,
// avoiding sending the literal "%session_id%" to upstream and causing dirty data.
func renderHeaderValue(value, sessionID string) (string, bool) {
	if !strings.Contains(value, sessionHeaderPlaceholder) {
		return value, true
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "", false
	}
	return strings.ReplaceAll(value, sessionHeaderPlaceholder, id), true
}

// normalizedCustomHeaders returns the normalized custom request headers: leading and
// trailing whitespace is trimmed from names, and empty names/values are discarded,
// so it is safe to write into http.Header.
func normalizedCustomHeaders(cfg *config.OpenAIConfig) map[string]string {
	if cfg == nil || len(cfg.Headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(cfg.Headers))
	for name, value := range cfg.Headers {
		name = strings.TrimSpace(name)
		if name == "" || strings.TrimSpace(value) == "" {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyCustomHeaders writes the channel-configured custom request headers onto the
// request.
// A built-in header with the same name (Authorization, Content-Type, etc.) is
// overridden by the configured value, so a third-party OpenAI-compatible gateway that
// only accepts a private auth header can still be called successfully.
// %session_id% in a value is replaced with the current conversation ID from the
// request context.
func applyCustomHeaders(req *http.Request, cfg *config.OpenAIConfig) {
	if req == nil || req.Header == nil {
		return
	}
	headers := normalizedCustomHeaders(cfg)
	if len(headers) == 0 {
		return
	}
	sessionID := SessionIDFromContext(req.Context())
	for name, value := range headers {
		rendered, ok := renderHeaderValue(value, sessionID)
		if !ok {
			continue
		}
		req.Header.Set(name, rendered)
	}
}

// headersRoundTripper injects the channel's custom request headers into requests that go through the Eino ChatModel.
type headersRoundTripper struct {
	base http.RoundTripper
	cfg  *config.OpenAIConfig
}

func (rt *headersRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := http.DefaultTransport
	if rt != nil && rt.base != nil {
		base = rt.base
	}
	if rt == nil || req == nil {
		return base.RoundTrip(req)
	}
	cloned := req.Clone(req.Context())
	applyCustomHeaders(cloned, rt.cfg)
	return base.RoundTrip(cloned)
}
