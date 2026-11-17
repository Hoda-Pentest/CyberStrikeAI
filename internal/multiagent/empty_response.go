package multiagent

import (
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

const defaultEmptyResponseContinueMaxAttempts = 5

// IsEmptyResponseResult reports whether the Run ended with the "no assistant body captured" placeholder (not a real user-visible reply).
func IsEmptyResponseResult(result *RunResult) bool {
	if result == nil {
		return false
	}
	return isEmptyResponseText(result.Response)
}

func isEmptyResponseText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Contains(s, "no assistant text was captured") ||
		strings.Contains(s, "no assistant text output was captured") ||
		strings.Contains(s, "未捕获到助手文本输出") // legacy zh placeholder (upstream)
}

// HasResumeTrace: only a non-empty trace has context to resume from.
func HasResumeTrace(result *RunResult) bool {
	if result == nil {
		return false
	}
	s := strings.TrimSpace(result.LastAgentTraceInput)
	return s != "" && s != "[]" && s != "null"
}

// EmptyResponseContinueMaxAttemptsFromConfig is the Handler-level backoff-continue cap when there is no assistant body; 0 = default 5.
func EmptyResponseContinueMaxAttemptsFromConfig(mw *config.AgentTuningConfig) int {
	if mw != nil && mw.EmptyResponseContinueMaxAttempts > 0 {
		return mw.EmptyResponseContinueMaxAttempts
	}
	return defaultEmptyResponseContinueMaxAttempts
}

// EmptyResponseContinueBackoff uses the same exponential backoff as run_retry (2s, 4s, 8s… capped).
func EmptyResponseContinueBackoff(attempt int, mw *config.AgentTuningConfig) time.Duration {
	maxBackoff := defaultRunRetryMaxBackoff
	if mw != nil && mw.RunRetryMaxBackoffSec > 0 {
		maxBackoff = time.Duration(mw.RunRetryMaxBackoffSec) * time.Second
	}
	return transientRetryBackoff(attempt, maxBackoff)
}

// FormatEmptyResponseContinueUserMessage is the user turn injected when the system auto-continues (not written as a bubble in the messages table).
func FormatEmptyResponseContinueUserMessage() string {
	return strings.TrimSpace(`[System auto-resume / Auto resume]
The previous Agent session produced no visible assistant body (the stream may have been interrupted, or only tool calls completed). Continue based on the existing trace and tool results and give a progress summary; do not repeat completed steps.`)
}
