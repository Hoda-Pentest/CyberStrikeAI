package multiagent

import (
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

const defaultEmptyResponseContinueMaxAttempts = 5

// IsEinoEmptyResponseResult decides whether a Run ended with the "no assistant body captured" placeholder (not a real user-visible reply).
func IsEinoEmptyResponseResult(result *RunResult) bool {
	if result == nil {
		return false
	}
	return isEinoEmptyResponseText(result.Response)
}

func isEinoEmptyResponseText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Contains(s, "no assistant text was captured") ||
		strings.Contains(s, "no assistant text output was captured")
}

// HasEinoResumeTrace: only a non-empty trace gives resumption context to restore.
func HasEinoResumeTrace(result *RunResult) bool {
	if result == nil {
		return false
	}
	s := strings.TrimSpace(result.LastAgentTraceInput)
	return s != "" && s != "[]" && s != "null"
}

// EmptyResponseContinueMaxAttemptsFromConfig is the Handler-layer backoff-continue cap when there is no assistant body; 0 = default 5.
func EmptyResponseContinueMaxAttemptsFromConfig(mw *config.MultiAgentEinoMiddlewareConfig) int {
	if mw != nil && mw.EmptyResponseContinueMaxAttempts > 0 {
		return mw.EmptyResponseContinueMaxAttempts
	}
	return defaultEmptyResponseContinueMaxAttempts
}

// EmptyResponseContinueBackoff uses the same exponential backoff as run_retry (2s, 4s, 8s... capped).
func EmptyResponseContinueBackoff(attempt int, mw *config.MultiAgentEinoMiddlewareConfig) time.Duration {
	maxBackoff := defaultEinoRunRetryMaxBackoff
	if mw != nil && mw.RunRetryMaxBackoffSec > 0 {
		maxBackoff = time.Duration(mw.RunRetryMaxBackoffSec) * time.Second
	}
	return einoTransientRetryBackoff(attempt, maxBackoff)
}

// FormatEmptyResponseContinueUserMessage is the user turn injected when the system auto-continues (not written as a bubble in the messages table).
func FormatEmptyResponseContinueUserMessage() string {
	return strings.TrimSpace(`[System auto-resume / Auto resume]
The previous Eino session produced no visible assistant body (possibly a streaming interruption or only tool calls completed). Please continue based on the existing trace and tool results, and give a staged summary; do not repeat completed steps.`)
}
