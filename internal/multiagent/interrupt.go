package multiagent

import "errors"

// ErrInterruptContinue is used as a context.CancelCause: when the user chooses "interrupt and continue" and there is no in-progress MCP tool,
// it cancels the current reasoning/streaming output and auto-continues the next round within the same session task carrying the user's supplementary note (a Hermes-style human turn).
var ErrInterruptContinue = errors.New("agent interrupt: continue with user-supplied context")
