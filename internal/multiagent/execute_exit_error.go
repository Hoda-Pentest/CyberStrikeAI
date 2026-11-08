package multiagent

import "fmt"

// ExecuteExitError indicates a non-zero exit of the execute command (an expected failure, not a timeout/interruption/stream exception).
type ExecuteExitError struct {
	Code int
}

func (e *ExecuteExitError) Error() string {
	if e == nil {
		return "exit status unknown"
	}
	return fmt.Sprintf("exit status %d", e.Code)
}
