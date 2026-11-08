package security

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// FormatCommandFailureResult matches the wording of the exec tool's ToolResult (without ToolErrorPrefix).
func FormatCommandFailureResult(exitCode int, output string) string {
	output = strings.TrimSpace(output)
	errMsg := fmt.Sprintf("exit status %d", exitCode)
	if output == "" {
		return fmt.Sprintf("Command execution failed: %s", errMsg)
	}
	if strings.HasPrefix(output, "Command execution failed:") {
		return output
	}
	return fmt.Sprintf("Command execution failed: %s\nOutput: %s", errMsg, output)
}

// FormatCommandFailureFromErr builds a unified failure message (the IsError body) from the error returned by exec/execute.
func FormatCommandFailureFromErr(err error, output string) string {
	if err == nil {
		return strings.TrimSpace(output)
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return FormatCommandFailureResult(exitError.ExitCode(), output)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Sprintf("Command execution failed: %v", err)
	}
	if strings.HasPrefix(output, "Command execution failed:") {
		return output
	}
	return fmt.Sprintf("Command execution failed: %v\nOutput: %s", err, output)
}

// ExecuteFailureStatusLine is the single-line status appended when a streaming execute ends (the output body has already been pushed in the stream).
func ExecuteFailureStatusLine(exitCode int) string {
	return fmt.Sprintf("\nCommand execution failed: exit status %d", exitCode)
}

// IsCommandFailureResult decides whether a tool-result body indicates a non-zero command exit (used to align isError between execute / exec).
func IsCommandFailureResult(content string) bool {
	return strings.Contains(content, "Command execution failed:")
}

// IsLegacyShellExitNoise filters out redundant exit-code lines in legacy shell streams.
func IsLegacyShellExitNoise(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "command exited with non-zero code ")
}
