package security

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/tooloutput"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ToolOutputCallback is used to push stdout/stderr increments to the upper layer (SSE) during tool execution.
// It is passed via context, avoiding the "hard-coded tool" problem caused by modifying the MCP ToolHandler signature.
type ToolOutputCallback func(chunk string)

type toolOutputCallbackCtxKey struct{}

// ToolOutputCallbackCtxKey is the key in context; the Agent writes the callback and the Executor reads it and calls back in a streaming fashion.
var ToolOutputCallbackCtxKey = toolOutputCallbackCtxKey{}

// Executor is the secure tool executor
type Executor struct {
	config                  *config.SecurityConfig
	toolIndex               map[string]*config.ToolConfig // tool index, for O(1) lookup
	mcpServer               *mcp.Server
	logger                  *zap.Logger
	shellNoOutputTimeoutSec int // idle seconds with no new output for execute/exec; 0 = default 300; -1 = disabled (see SetShellNoOutputTimeoutSeconds)
	toolOutputMaxBytes      int
	spillRootDir            string
}

// NewExecutor creates a new executor
func NewExecutor(cfg *config.SecurityConfig, mcpServer *mcp.Server, logger *zap.Logger) *Executor {
	executor := &Executor{
		config:    cfg,
		toolIndex: make(map[string]*config.ToolConfig),
		mcpServer: mcpServer,
		logger:    logger,
	}
	// Build the tool index
	executor.buildToolIndex()
	return executor
}

// SetShellNoOutputTimeoutSeconds configures the no-output idle termination of the exec tool (consistent with agent.shell_no_output_timeout_seconds).
func (e *Executor) SetShellNoOutputTimeoutSeconds(sec int) {
	e.shellNoOutputTimeoutSec = sec
}

// SetToolOutputMaxBytes limits stdout/stderr retained and streamed by exec-like
// tools. It should stay aligned with MCP result normalization so every channel
// sees the same bounded payload. Oversized full output is spilled to disk first.
func (e *Executor) SetToolOutputMaxBytes(maxBytes int) {
	e.toolOutputMaxBytes = maxBytes
}

// SetToolOutputSpillRoot sets the reduction-compatible root for spilling full
// exec stdout/stderr when the in-memory bound is exceeded (empty → tmp/reduction).
func (e *Executor) SetToolOutputSpillRoot(rootDir string) {
	e.spillRootDir = strings.TrimSpace(rootDir)
}

func (e *Executor) wrapToolOutputCallback(ctx context.Context, cb ToolOutputCallback) ToolOutputCallback {
	executionID := mcp.MCPExecutionIDFromContext(ctx)
	if e == nil || e.mcpServer == nil || strings.TrimSpace(executionID) == "" {
		return cb
	}
	return func(chunk string) {
		if chunk != "" {
			e.mcpServer.AppendToolExecutionPartialOutput(executionID, chunk)
		}
		if cb != nil {
			cb(chunk)
		}
	}
}

func (e *Executor) spillOptsFromContext(ctx context.Context) tooloutput.SpillOpts {
	root := ""
	if e != nil {
		root = e.spillRootDir
	}
	opts := tooloutput.SpillOpts{RootDir: root}
	if ctx != nil {
		opts.ConversationID = mcp.MCPConversationIDFromContext(ctx)
		opts.ProjectID = mcp.MCPProjectIDFromContext(ctx)
		opts.ExecutionID = mcp.MCPExecutionIDFromContext(ctx)
	}
	if opts.ExecutionID == "" {
		opts.ExecutionID = uuid.NewString()
	}
	return opts
}

// buildToolIndex builds the tool index, optimizing O(n) lookup to O(1)
func (e *Executor) buildToolIndex() {
	e.toolIndex = make(map[string]*config.ToolConfig)
	for i := range e.config.Tools {
		if e.config.Tools[i].Enabled {
			e.toolIndex[e.config.Tools[i].Name] = &e.config.Tools[i]
		}
	}
	e.logger.Debug("tool index built",
		zap.Int("totalTools", len(e.config.Tools)),
		zap.Int("enabledTools", len(e.toolIndex)),
	)
}

// ExecuteTool executes a security tool
func (e *Executor) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*mcp.ToolResult, error) {
	e.logger.Debug("ExecuteTool called",
		zap.String("toolName", toolName),
		zap.Any("args", args),
	)

	// Special case: the exec tool executes system commands directly
	if toolName == "exec" {
		e.logger.Debug("executing the exec tool")
		return e.executeSystemCommand(ctx, args)
	}

	// Look up the tool configuration via the index (O(1) lookup)
	toolConfig, exists := e.toolIndex[toolName]
	if !exists {
		e.logger.Error("tool not found or not enabled",
			zap.String("toolName", toolName),
			zap.Int("totalTools", len(e.config.Tools)),
			zap.Int("enabledTools", len(e.toolIndex)),
		)
		return nil, fmt.Errorf("tool %s not found or not enabled", toolName)
	}

	e.logger.Debug("found tool configuration",
		zap.String("toolName", toolName),
		zap.String("command", toolConfig.Command),
		zap.Strings("args", toolConfig.Args),
	)

	// Special case: internal tools (command starting with "internal:")
	if strings.HasPrefix(toolConfig.Command, "internal:") {
		e.logger.Debug("executing internal tool",
			zap.String("toolName", toolName),
			zap.String("command", toolConfig.Command),
		)
		return e.executeInternalTool(ctx, toolName, toolConfig.Command, args)
	}

	// Build the command - use different argument formats depending on the tool type
	cmdArgs := e.buildCommandArgs(toolName, toolConfig, args)

	e.logger.Debug("command arguments built",
		zap.String("toolName", toolName),
		zap.Strings("cmdArgs", cmdArgs),
		zap.Int("argsCount", len(cmdArgs)),
	)

	// Validate the command arguments
	if len(cmdArgs) == 0 {
		e.logger.Warn("command arguments are empty",
			zap.String("toolName", toolName),
			zap.Any("inputArgs", args),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Error: tool %s is missing required arguments. Received arguments: %v", toolName, args),
				},
			},
			IsError: true,
		}, nil
	}

	// Execute the command
	cmd := exec.CommandContext(ctx, toolConfig.Command, cmdArgs...)
	applyDefaultTerminalEnv(cmd)
	attachNonInteractiveStdin(cmd)
	_ = prepareShellCmdSession(cmd)

	e.logger.Debug("executing security tool",
		zap.String("tool", toolName),
		zap.Strings("args", cmdArgs),
	)

	var output string
	var err error
	spill := e.spillOptsFromContext(ctx)
	// If the upper layer provided a stdout/stderr increment callback, or we are in an MCP execution, read and call back while executing.
	if cb, ok := ctx.Value(ToolOutputCallbackCtxKey).(ToolOutputCallback); (ok && cb != nil) || mcp.MCPExecutionIDFromContext(ctx) != "" {
		cb = e.wrapToolOutputCallback(ctx, cb)
		output, err = streamCommandOutput(ctx, cmd, cb, ResolveShellNoOutputTimeoutSeconds(e.shellNoOutputTimeoutSec), e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("detected that the tool needs a TTY, retrying with a PTY",
				zap.String("tool", toolName),
			)
			cmd2 := exec.CommandContext(ctx, toolConfig.Command, cmdArgs...)
			applyDefaultTerminalEnv(cmd2)
			_ = prepareShellCmdSession(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, cb, e.toolOutputMaxBytes, spill)
		}
	} else {
		// Non-streaming: in-memory buffer + killing the process group on ctx cancel; behavior aligned with the original CombinedOutput, avoiding a two-stream pipe fan-in deadlock.
		output, err = combinedOutputCancellableWithLimit(ctx, cmd, e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("detected that the tool needs a TTY, retrying with a PTY",
				zap.String("tool", toolName),
			)
			cmd2 := exec.CommandContext(ctx, toolConfig.Command, cmdArgs...)
			applyDefaultTerminalEnv(cmd2)
			_ = prepareShellCmdSession(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, nil, e.toolOutputMaxBytes, spill)
		}
	}
	if err != nil {
		// Check whether the exit code is in the allowed list
		exitCode := getExitCode(err)
		if exitCode != nil && toolConfig.AllowedExitCodes != nil {
			for _, allowedCode := range toolConfig.AllowedExitCodes {
				if *exitCode == allowedCode {
					e.logger.Debug("tool execution completed (exit code in the allowed list)",
						zap.String("tool", toolName),
						zap.Int("exitCode", *exitCode),
						zap.String("output", string(output)),
					)
					return &mcp.ToolResult{
						Content: []mcp.Content{
							{
								Type: "text",
								Text: string(output),
							},
						},
						IsError: false,
					}, nil
				}
			}
		}

		e.logger.Error("tool execution failed",
			zap.String("tool", toolName),
			zap.Error(err),
			zap.Int("exitCode", getExitCodeValue(err)),
			zap.String("output", string(output)),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Tool execution failed: %v\nOutput: %s", err, string(output)),
				},
			},
			IsError: true,
		}, nil
	}

	e.logger.Debug("tool execution succeeded",
		zap.String("tool", toolName),
		zap.String("output", string(output)),
	)

	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: string(output),
			},
		},
		IsError: false,
	}, nil
}

// RegisterTools registers tools with the MCP server
func (e *Executor) RegisterTools(mcpServer *mcp.Server) {
	e.logger.Debug("starting to register tools",
		zap.Int("totalTools", len(e.config.Tools)),
		zap.Int("enabledTools", len(e.toolIndex)),
	)

	// Rebuild the index (in case the configuration was updated)
	e.buildToolIndex()

	for i, toolConfig := range e.config.Tools {
		if !toolConfig.Enabled {
			e.logger.Debug("skipping a disabled tool",
				zap.String("tool", toolConfig.Name),
			)
			continue
		}

		// Create a copy of the tool configuration to avoid closure issues
		toolName := toolConfig.Name
		toolConfigCopy := toolConfig

		// Decide which description to expose to the AI/API based on configuration: short_description or description
		useFullDescription := strings.TrimSpace(strings.ToLower(e.config.ToolDescriptionMode)) == "full"
		shortDesc := toolConfigCopy.ShortDescription
		if shortDesc == "" {
			// If there is no short description, extract the first line or the first 10000 characters from the detailed description
			desc := toolConfigCopy.Description
			if len(desc) > 10000 {
				if idx := strings.Index(desc, "\n"); idx > 0 && idx < 10000 {
					shortDesc = strings.TrimSpace(desc[:idx])
				} else {
					shortDesc = desc[:10000] + "..."
				}
			} else {
				shortDesc = desc
			}
		}
		if useFullDescription {
			shortDesc = "" // Clear ShortDescription when using description; downstream falls back to Description
		}

		tool := mcp.Tool{
			Name:             toolConfigCopy.Name,
			Description:      toolConfigCopy.Description,
			ShortDescription: shortDesc,
			InputSchema:      e.buildInputSchema(&toolConfigCopy),
		}

		handler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
			e.logger.Debug("tool handler called",
				zap.String("toolName", toolName),
				zap.Any("args", args),
			)
			return e.ExecuteTool(ctx, toolName, args)
		}

		mcpServer.RegisterTool(tool, handler)
		e.logger.Debug("registered security tool successfully",
			zap.String("tool", toolConfigCopy.Name),
			zap.String("command", toolConfigCopy.Command),
			zap.Int("index", i),
		)
	}

	e.logger.Debug("tool registration completed",
		zap.Int("registeredCount", len(e.config.Tools)),
	)
}

// buildCommandArgs builds the command arguments
func (e *Executor) buildCommandArgs(toolName string, toolConfig *config.ToolConfig, args map[string]interface{}) []string {
	cmdArgs := make([]string, 0)

	// If the configuration defines an argument mapping, use the mapping rules from the configuration
	if len(toolConfig.Parameters) > 0 {
		// Check for a scan_type argument; if present, replace the default scan-type arguments
		hasScanType := false
		var scanTypeValue string
		if scanType, ok := args["scan_type"].(string); ok && scanType != "" {
			hasScanType = true
			scanTypeValue = scanType
		}

		// Add fixed arguments (if scan_type is specified, the default scan-type arguments may need to be filtered out)
		if hasScanType && toolName == "nmap" {
			// For nmap, if scan_type is specified, skip the default -sT -sV -sC
			// These arguments are replaced by the scan_type argument
		} else {
			cmdArgs = append(cmdArgs, toolConfig.Args...)
		}

		// Sort by positional argument
		positionalParams := make([]config.ParameterConfig, 0)
		flagParams := make([]config.ParameterConfig, 0)

		for _, param := range toolConfig.Parameters {
			if param.Position != nil {
				positionalParams = append(positionalParams, param)
			} else {
				flagParams = append(flagParams, param)
			}
		}

		// For tools that need a subcommand (such as gobuster dir), position 0 must come right after the command name and before all flags
		for _, param := range positionalParams {
			if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
				continue
			}
			if param.Position != nil && *param.Position == 0 {
				value := e.getParamValue(args, param)
				if value == nil && param.Default != nil {
					value = param.Default
				}
				if value != nil {
					cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
				}
				break
			}
		}

		// Process flag arguments
		for _, param := range flagParams {
			// Skip special arguments; they are handled separately later
			// The action argument is only for internal tool logic and is not passed to the command
			if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
				continue
			}

			value := e.getParamValue(args, param)
			if value == nil {
				if param.Required {
					// A required argument is missing; return an empty array and let the upper layer handle the error
					e.logger.Warn("missing a required flag argument",
						zap.String("tool", toolName),
						zap.String("param", param.Name),
					)
					return []string{}
				}
				continue
			}

			// Boolean special handling: if false, skip; if true, add only the flag
			if param.Type == "bool" {
				var boolVal bool
				var ok bool

				// Try multiple type conversions
				if boolVal, ok = value.(bool); ok {
					// Already a boolean
				} else if numVal, ok := value.(float64); ok {
					// JSON number type (float64)
					boolVal = numVal != 0
					ok = true
				} else if numVal, ok := value.(int); ok {
					// int type
					boolVal = numVal != 0
					ok = true
				} else if strVal, ok := value.(string); ok {
					// string type
					boolVal = strVal == "true" || strVal == "1" || strVal == "yes"
					ok = true
				}

				if ok {
					if !boolVal {
						continue // add nothing when false
					}
					// When true, add only the flag, not the value
					if param.Flag != "" {
						cmdArgs = append(cmdArgs, param.Flag)
					}
					continue
				}
			}

			formattedValue := e.formatParamValue(param, value)
			if strings.TrimSpace(formattedValue) == "" {
				if param.Required {
					e.logger.Warn("a required argument is empty",
						zap.String("tool", toolName),
						zap.String("param", param.Name),
					)
					return []string{}
				}
				continue
			}

			format := param.Format
			if format == "" {
				format = "flag" // default format
			}

			switch format {
			case "flag":
				// --flag value or -f value
				if param.Flag != "" {
					cmdArgs = append(cmdArgs, param.Flag)
				}
				cmdArgs = append(cmdArgs, formattedValue)
			case "combined":
				// --flag=value or -f=value
				if param.Flag != "" {
					cmdArgs = append(cmdArgs, fmt.Sprintf("%s=%s", param.Flag, formattedValue))
				} else {
					cmdArgs = append(cmdArgs, formattedValue)
				}
			case "template":
				// Use a template string
				if param.Template != "" {
					template := param.Template
					template = strings.ReplaceAll(template, "{flag}", param.Flag)
					template = strings.ReplaceAll(template, "{value}", formattedValue)
					template = strings.ReplaceAll(template, "{name}", param.Name)
					cmdArgs = append(cmdArgs, strings.Fields(template)...)
				} else {
					// If there is no template, use the default format
					if param.Flag != "" {
						cmdArgs = append(cmdArgs, param.Flag)
					}
					cmdArgs = append(cmdArgs, formattedValue)
				}
			case "positional":
				// Positional argument (already handled above)
				cmdArgs = append(cmdArgs, formattedValue)
			default:
				// Default: add the value directly
				cmdArgs = append(cmdArgs, formattedValue)
			}
		}

		// Then process positional arguments (positional arguments usually come after flag arguments)
		// Sort positional arguments by position
		// First find the maximum position value to determine how many positions need processing
		maxPosition := -1
		for _, param := range positionalParams {
			if param.Position != nil && *param.Position > maxPosition {
				maxPosition = *param.Position
			}
		}

		// Process arguments in position order, so that even if some positions have no argument or use a default, they are passed correctly
		// position 0 was already inserted earlier (subcommand first); start from 1 here
		for i := 0; i <= maxPosition; i++ {
			if i == 0 {
				continue
			}
			for _, param := range positionalParams {
				// Skip special arguments; they are handled separately later
				// The action argument is only for internal tool logic and is not passed to the command
				if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
					continue
				}

				if param.Position != nil && *param.Position == i {
					value := e.getParamValue(args, param)
					if value == nil {
						if param.Required {
							// A required argument is missing; return an empty array and let the upper layer handle the error
							e.logger.Warn("missing a required positional argument",
								zap.String("tool", toolName),
								zap.String("param", param.Name),
								zap.Int("position", *param.Position),
							)
							return []string{}
						}
						// For non-required arguments, if the value is nil, try using the default value
						if param.Default != nil {
							value = param.Default
						} else {
							// If there is no default value, skip this position and continue with the next
							break
						}
					}
					// Only add to the command arguments when the value is not nil
					if value != nil {
						cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
					}
					break
				}
			}
			// If no argument is found for a position, continue with the next position
			// This ensures the positional arguments are in the correct order
		}

		// Special case: the additional_args argument (needs to be split by spaces into multiple arguments)
		if additionalArgs, ok := args["additional_args"].(string); ok && additionalArgs != "" {
			// Split by spaces but keep the content inside quotes
			additionalArgsList := e.parseAdditionalArgs(additionalArgs)
			cmdArgs = append(cmdArgs, additionalArgsList...)
		}

		// Special case: the scan_type argument (needs to be split by spaces and inserted at the right position)
		if hasScanType {
			scanTypeArgs := e.parseAdditionalArgs(scanTypeValue)
			if len(scanTypeArgs) > 0 {
				if toolName == "nmap" {
					// Keep each option next to its value (especially --script).
					// Nmap accepts scan options before all other arguments.
					return append(scanTypeArgs, cmdArgs...)
				}
				// Preserve the existing insertion rule for other tools using scan_type.
				insertPos := len(cmdArgs)
				for i := len(cmdArgs) - 1; i >= 0; i-- {
					// target is usually the last non-flag argument
					if !strings.HasPrefix(cmdArgs[i], "-") {
						insertPos = i
						break
					}
				}
				// Insert the scan_type arguments before target
				newArgs := make([]string, 0, len(cmdArgs)+len(scanTypeArgs))
				newArgs = append(newArgs, cmdArgs[:insertPos]...)
				newArgs = append(newArgs, scanTypeArgs...)
				newArgs = append(newArgs, cmdArgs[insertPos:]...)
				cmdArgs = newArgs
			}
		}

		return cmdArgs
	}

	// If no argument configuration is defined, use the fixed arguments and generic handling
	// Add fixed arguments
	cmdArgs = append(cmdArgs, toolConfig.Args...)

	// Generic handling: convert arguments to command-line arguments
	for key, value := range args {
		if key == "_tool_name" {
			continue
		}
		// Use the --key value format
		cmdArgs = append(cmdArgs, fmt.Sprintf("--%s", key))
		if strValue, ok := value.(string); ok {
			cmdArgs = append(cmdArgs, strValue)
		} else {
			cmdArgs = append(cmdArgs, fmt.Sprintf("%v", value))
		}
	}

	return cmdArgs
}

// parseAdditionalArgs parses the additional_args string, splitting by spaces but keeping the content inside quotes
func (e *Executor) parseAdditionalArgs(argsStr string) []string {
	if argsStr == "" {
		return []string{}
	}

	result := make([]string, 0)
	var current strings.Builder
	inQuotes := false
	var quoteChar rune
	escapeNext := false

	runes := []rune(argsStr)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' {
			// Check whether the next character is a quote
			if i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\'') {
				// Escaped quote: skip the backslash and write the quote as an ordinary character
				i++
				current.WriteRune(runes[i])
			} else {
				// Other escape characters: write the backslash; the next character is handled in the next iteration
				escapeNext = true
				current.WriteRune(r)
			}
			continue
		}

		if !inQuotes && (r == '"' || r == '\'') {
			inQuotes = true
			quoteChar = r
			continue
		}

		if inQuotes && r == quoteChar {
			inQuotes = false
			quoteChar = 0
			continue
		}

		if !inQuotes && (r == ' ' || r == '\t' || r == '\n') {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	// Handle the last argument (if any)
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	// If the parse result is empty, fall back to simple space splitting
	if len(result) == 0 {
		result = strings.Fields(argsStr)
	}

	return result
}

// getParamValue gets an argument value, with default-value support
func (e *Executor) getParamValue(args map[string]interface{}, param config.ParameterConfig) interface{} {
	// Get the value from the arguments
	if value, ok := args[param.Name]; ok && value != nil {
		return value
	}

	// If the argument is required but not provided, return nil (let the upper layer handle the error)
	if param.Required {
		return nil
	}

	// Return the default value
	return param.Default
}

// formatParamValue formats an argument value
func (e *Executor) formatParamValue(param config.ParameterConfig, value interface{}) string {
	switch param.Type {
	case "bool":
		// Booleans should be handled by the upper layer; this should not be called
		if boolVal, ok := value.(bool); ok {
			return fmt.Sprintf("%v", boolVal)
		}
		return "false"
	case "array":
		// Array: convert to a comma-separated string
		if arr, ok := value.([]interface{}); ok {
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				strs = append(strs, fmt.Sprintf("%v", item))
			}
			return strings.Join(strs, ",")
		}
		return fmt.Sprintf("%v", value)
	case "object":
		// Object/dict: serialize to a JSON string
		if jsonBytes, err := json.Marshal(value); err == nil {
			return string(jsonBytes)
		}
		// If JSON serialization fails, fall back to default formatting
		return fmt.Sprintf("%v", value)
	default:
		formattedValue := fmt.Sprintf("%v", value)
		// Special case: for the ports argument (usually the port argument of tools like nmap), strip spaces
		// nmap does not accept spaces in a port list, e.g. "80,443, 22" should become "80,443,22"
		if param.Name == "ports" {
			// Remove all spaces but keep commas and other characters
			formattedValue = strings.ReplaceAll(formattedValue, " ", "")
		}
		return formattedValue
	}
}

// IsBackgroundShellCommand detects whether a command is fully backgrounded (has a standalone & at the end, not inside quotes).
// command1 & command2 does not count as fully backgrounded (command2 still runs in the foreground).
func IsBackgroundShellCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	positions := findStandaloneAmpersandPositions(command)
	if len(positions) == 0 {
		return false
	}
	last := positions[len(positions)-1]
	afterAmpersand := strings.TrimSpace(command[last+1:])
	if afterAmpersand != "" {
		return false
	}
	beforeAmpersand := strings.TrimSpace(command[:last])
	return beforeAmpersand != ""
}

// executeSystemCommand executes a system command
func (e *Executor) executeSystemCommand(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
	// Get the command
	command, ok := args["command"].(string)
	if !ok {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "Error: missing the command argument",
				},
			},
			IsError: true,
		}, nil
	}

	if command == "" {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "Error: the command argument must not be empty",
				},
			},
			IsError: true,
		}, nil
	}

	// Security check: log the command being executed
	e.logger.Warn("executing system command",
		zap.String("command", command),
	)

	command = PrepareShellCommandForExecute(command)

	// Get the shell type (optional, default sh)
	shell := "sh"
	if s, ok := args["shell"].(string); ok && s != "" {
		shell = s
	}

	// Get the working directory (optional)
	workDir := ""
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		workDir = wd
	}

	// Detect whether it is a background command (contains an & that is not inside quotes)
	isBackground := IsBackgroundShellCommand(command)

	// Build the command
	var cmd *exec.Cmd
	if workDir != "" {
		cmd = exec.CommandContext(ctx, shell, "-c", command)
		cmd.Dir = workDir
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	}
	ConfigureShellCmdForAgentExecute(cmd)

	// Execute the command
	e.logger.Info("executing system command",
		zap.String("command", command),
		zap.String("shell", shell),
		zap.String("workdir", workDir),
		zap.Bool("isBackground", isBackground),
	)

	if isBackground {
		job := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(command), "&"))
		session, err := StartManagedBackground(ctx, shell, job, workDir)
		if err != nil {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf("failed to start background command: %v", err)}}, IsError: true}, nil
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf("Background command started\nCommand: %s\nProcess group ID: %d\n\nThe background process is managed by this task and is cleaned up automatically when the task ends.", command, session.rootPID)}}}, nil
	}

	// Non-background command: wait for output
	var output string
	var err error
	spill := e.spillOptsFromContext(ctx)
	// If the upper layer provided a tool-output increment callback, or we are in an MCP execution, read in a streaming fashion while executing.
	if cb, ok := ctx.Value(ToolOutputCallbackCtxKey).(ToolOutputCallback); (ok && cb != nil) || mcp.MCPExecutionIDFromContext(ctx) != "" {
		cb = e.wrapToolOutputCallback(ctx, cb)
		output, err = streamCommandOutput(ctx, cmd, cb, ResolveShellNoOutputTimeoutSeconds(e.shellNoOutputTimeoutSec), e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("detected that the system command needs a TTY, retrying with a PTY")
			cmd2 := exec.CommandContext(ctx, shell, "-c", command)
			if workDir != "" {
				cmd2.Dir = workDir
			}
			ConfigureShellCmdForAgentExecute(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, cb, e.toolOutputMaxBytes, spill)
		}
	} else {
		output, err = combinedOutputCancellableWithLimit(ctx, cmd, e.toolOutputMaxBytes, spill)
		if err != nil && shouldRetryWithPTY(output) {
			e.logger.Info("detected that the system command needs a TTY, retrying with a PTY")
			cmd2 := exec.CommandContext(ctx, shell, "-c", command)
			if workDir != "" {
				cmd2.Dir = workDir
			}
			ConfigureShellCmdForAgentExecute(cmd2)
			output, err = runCommandWithPTY(ctx, cmd2, nil, e.toolOutputMaxBytes, spill)
		}
	}
	if err != nil {
		e.logger.Error("system command execution failed",
			zap.String("command", command),
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: FormatCommandFailureFromErr(err, output),
				},
			},
			IsError: true,
		}, nil
	}

	e.logger.Info("system command executed successfully",
		zap.String("command", command),
		zap.String("output_length", fmt.Sprintf("%d", len(output))),
	)

	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: string(output),
			},
		},
		IsError: false,
	}, nil
}

// combinedOutputCancellable aligns with cmd.CombinedOutput (stdout/stderr written to an in-memory buffer),
// but on ctx cancel, terminateCmdTree terminates the whole process tree.
// The non-streaming path does not use two-stream pipe fan-in, avoiding a deadlock where stdout and stderr block each other when stderr fills the pipe buffer.
// No-output idle detection is backstopped by the upper layer's agent.tool_timeout_minutes, without changing the original CombinedOutput semantics.
func combinedOutputCancellable(ctx context.Context, cmd *exec.Cmd) (string, error) {
	return combinedOutputCancellableWithLimit(ctx, cmd, 0, tooloutput.SpillOpts{})
}

func combinedOutputCancellableWithLimit(ctx context.Context, cmd *exec.Cmd, maxBytes int, spill tooloutput.SpillOpts) (string, error) {
	var tee *tooloutput.Tee
	if maxBytes > 0 {
		tee = tooloutput.NewTee(spill)
		defer func() { _ = tee.Close() }()
	}
	stdoutBuf := newBoundedOutputCollector(maxBytes, tee)
	stderrBuf := newBoundedOutputCollector(maxBytes, tee)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	session, err := StartShellSessionContext(ctx, cmd)
	if err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			TerminateShellCmdSession(session)
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		waitErr = <-done
		return finalizeJoinedBoundedOutputs(stdoutBuf, stderrBuf, maxBytes, tee), ctx.Err()
	}
	return finalizeJoinedBoundedOutputs(stdoutBuf, stderrBuf, maxBytes, tee), waitErr
}

func joinCommandOutput(stdout, stderr string) string {
	if stderr == "" {
		return stdout
	}
	if stdout == "" {
		return stderr
	}
	return stdout + stderr
}

type boundedOutputCollector struct {
	builder   strings.Builder
	maxBytes  int
	seenBytes int
	truncated bool
	tee       *tooloutput.Tee
}

func newBoundedOutputCollector(maxBytes int, tee *tooloutput.Tee) *boundedOutputCollector {
	return &boundedOutputCollector{maxBytes: maxBytes, tee: tee}
}

func (b *boundedOutputCollector) Write(p []byte) (int, error) {
	b.WriteStringLimited(string(p))
	return len(p), nil
}

func (b *boundedOutputCollector) WriteStringLimited(s string) string {
	if b == nil {
		return ""
	}
	if b.tee != nil {
		_, _ = b.tee.Write([]byte(s))
	}
	if b.maxBytes <= 0 {
		b.seenBytes += len(s)
		b.builder.WriteString(s)
		return s
	}
	b.seenBytes += len(s)
	if b.builder.Len() >= b.maxBytes {
		b.truncated = true
		return ""
	}
	remaining := b.maxBytes - b.builder.Len()
	if len(s) <= remaining {
		b.builder.WriteString(s)
		return s
	}
	kept := truncateStringBytes(s, remaining)
	b.builder.WriteString(kept)
	b.truncated = true
	return kept
}

func (b *boundedOutputCollector) String() string {
	if b == nil {
		return ""
	}
	return b.builder.String()
}

func finalizeJoinedBoundedOutputs(stdout, stderr *boundedOutputCollector, maxBytes int, tee *tooloutput.Tee) string {
	if tee != nil {
		_ = tee.Close()
	}
	truncated := (stdout != nil && stdout.truncated) || (stderr != nil && stderr.truncated)
	seen := 0
	if stdout != nil {
		seen += stdout.seenBytes
	}
	if stderr != nil {
		seen += stderr.seenBytes
	}
	joined := joinCommandOutput(
		func() string {
			if stdout == nil {
				return ""
			}
			return stdout.String()
		}(),
		func() string {
			if stderr == nil {
				return ""
			}
			return stderr.String()
		}(),
	)
	if maxBytes > 0 && !truncated && len(joined) > maxBytes {
		truncated = true
		seen = len(joined)
	}
	path := ""
	if tee != nil {
		path = tee.Path()
	}
	if truncated && maxBytes > 0 {
		if path != "" {
			return tooloutput.FormatPersistedFromFile(path, seen, maxBytes)
		}
		if len(joined) > maxBytes {
			return truncateStringBytes(joined, maxBytes)
		}
		return joined
	}
	if path != "" {
		_ = os.Remove(path)
	}
	if maxBytes > 0 && len(joined) > maxBytes {
		return truncateStringBytes(joined, maxBytes)
	}
	return joined
}

func finalizeBoundedOutput(collector *boundedOutputCollector, maxBytes int, tee *tooloutput.Tee) string {
	if tee != nil {
		_ = tee.Close()
	}
	if collector == nil {
		return ""
	}
	path := ""
	if tee != nil {
		path = tee.Path()
	}
	if collector.truncated && maxBytes > 0 {
		if path != "" {
			return tooloutput.FormatPersistedFromFile(path, collector.seenBytes, maxBytes)
		}
		return truncateStringBytes(collector.String(), maxBytes)
	}
	if path != "" {
		_ = os.Remove(path)
	}
	out := collector.String()
	if maxBytes > 0 && len(out) > maxBytes {
		return tooloutput.BoundWithSpill(out, maxBytes, tooloutput.SpillOpts{})
	}
	return out
}

func limitOutputString(s string, maxBytes int, spill tooloutput.SpillOpts) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	return tooloutput.BoundWithSpill(s, maxBytes, spill)
}

func truncateStringBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	if cut <= 0 {
		return ""
	}
	return s[:cut]
}

// streamCommandOutput reads the command's stdout/stderr in a "read and call back as you go" fashion.
// It reads fixed-size blocks, avoiding line-by-line reading that would block forever on output with no newlines; on ctx cancel it terminates the process tree.
func streamCommandOutput(ctx context.Context, cmd *exec.Cmd, cb ToolOutputCallback, noOutputSec int, maxBytes int, spill tooloutput.SpillOpts) (string, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdoutPipe.Close()
		return "", err
	}
	session, err := StartShellSessionContext(ctx, cmd)
	if err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return "", err
	}

	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			TerminateShellCmdSession(session)
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	readStop := make(chan struct{})
	defer close(readStop)
	chunks := make(chan string, 64)
	var wg sync.WaitGroup
	readFn := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 8192)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				select {
				case chunks <- string(buf[:n]):
				case <-readStop:
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}

	wg.Add(2)
	go readFn(stdoutPipe)
	go readFn(stderrPipe)

	go func() {
		wg.Wait()
		close(chunks)
	}()

	tee := (*tooloutput.Tee)(nil)
	if maxBytes > 0 {
		tee = tooloutput.NewTee(spill)
		defer func() { _ = tee.Close() }()
	}
	outBuilder := newBoundedOutputCollector(maxBytes, tee)
	var deltaBuilder strings.Builder
	lastFlush := time.Now()

	flush := func() {
		if deltaBuilder.Len() == 0 {
			return
		}
		if cb != nil {
			cb(deltaBuilder.String())
		}
		deltaBuilder.Reset()
		lastFlush = time.Now()
	}

	idleWatch := NewShellInactivityWatch(noOutputSec)
	if idleWatch != nil {
		defer idleWatch.Stop()
	}

	fireInactivity := func() {
		TerminateShellCmdSession(session)
		msg := ShellNoOutputTimeoutMessage(idleWatch.Sec)
		msg = outBuilder.WriteStringLimited(msg)
		if cb != nil {
			cb(msg)
		}
		_ = session.Wait()
	}

chunksLoop:
	for {
		var idleCh <-chan struct{}
		if idleWatch != nil {
			idleCh = idleWatch.Expired
		}
		select {
		case <-ctx.Done():
			TerminateShellCmdSession(session)
			flush()
			_ = session.Wait()
			return outBuilder.String(), ctx.Err()
		case <-idleCh:
			fireInactivity()
			return finalizeBoundedOutput(outBuilder, maxBytes, tee), fmt.Errorf("shell inactivity timeout (%ds)", idleWatch.Sec)
		case chunk, ok := <-chunks:
			if !ok {
				break chunksLoop
			}
			if chunk != "" && idleWatch != nil {
				idleWatch.Bump()
			}
			keptChunk := outBuilder.WriteStringLimited(chunk)
			deltaBuilder.WriteString(keptChunk)
			if deltaBuilder.Len() >= 2048 || time.Since(lastFlush) >= 200*time.Millisecond {
				flush()
			}
		}
	}
	flush()

	// Wait for the command to finish and return the final exit status
	waitErr := session.Wait()
	return finalizeBoundedOutput(outBuilder, maxBytes, tee), waitErr
}

// applyDefaultTerminalEnv adds common terminal environment variables for external tools.
// Note: this does not create a TTY; it only reduces "weird layout/detection failures" some tools have in a non-interactive environment.
func applyDefaultTerminalEnv(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	// Inherit the current process environment only when Env is not set explicitly
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = ApplyNonInteractivePagerEnv(cmd.Env)
	// If the user already set TERM/COLUMNS/LINES, do not override them
	has := func(k string) bool {
		prefix := k + "="
		for _, e := range cmd.Env {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}
	if !has("TERM") {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	if !has("COLUMNS") {
		cmd.Env = append(cmd.Env, "COLUMNS=256")
	}
	if !has("LINES") {
		cmd.Env = append(cmd.Env, "LINES=40")
	}
}

func shouldRetryWithPTY(output string) bool {
	o := strings.ToLower(output)
	// Common errors from autorecon / python termios
	if strings.Contains(o, "inappropriate ioctl for device") {
		return true
	}
	if strings.Contains(o, "termios.error") {
		return true
	}
	// Fallback: stdin is not a tty
	if strings.Contains(o, "not a tty") {
		return true
	}
	return false
}

// runCommandWithPTY allocates a PTY for the subprocess, for tools that need an interactive terminal (such as autorecon).
// If cb != nil, it continuously calls back incremental output (for SSE).
func runCommandWithPTY(ctx context.Context, cmd *exec.Cmd, cb ToolOutputCallback, maxBytes int, spill tooloutput.SpillOpts) (string, error) {
	if runtime.GOOS == "windows" {
		// The PTY approach is Unix-like; Windows uses the original logic
		if cb != nil {
			return streamCommandOutput(ctx, cmd, cb, 0, maxBytes, spill)
		}
		_ = prepareShellCmdSession(cmd)
		return combinedOutputCancellableWithLimit(ctx, cmd, maxBytes, spill)
	}

	_ = prepareShellCmdSession(cmd)
	var ptmx *os.File
	session, err := startShellSessionContext(ctx, cmd, func() error {
		var startErr error
		ptmx, startErr = pty.Start(cmd)
		return startErr
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = ptmx.Close() }()

	// Terminate the subprocess as soon as possible when ctx is cancelled
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = ptmx.Close() // trigger the read to exit
			session.Terminate()
		case <-done:
		}
	}()
	defer close(done)

	tee := (*tooloutput.Tee)(nil)
	if maxBytes > 0 {
		tee = tooloutput.NewTee(spill)
		defer func() { _ = tee.Close() }()
	}
	outBuilder := newBoundedOutputCollector(maxBytes, tee)
	var deltaBuilder strings.Builder
	lastFlush := time.Now()
	flush := func() {
		if cb == nil || deltaBuilder.Len() == 0 {
			deltaBuilder.Reset()
			lastFlush = time.Now()
			return
		}
		cb(deltaBuilder.String())
		deltaBuilder.Reset()
		lastFlush = time.Now()
	}

	buf := make([]byte, 4096)
	for {
		n, readErr := ptmx.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			// Normalize line endings to \n to avoid frontend misalignment
			chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
			chunk = strings.ReplaceAll(chunk, "\r", "\n")
			keptChunk := outBuilder.WriteStringLimited(chunk)
			deltaBuilder.WriteString(keptChunk)
			if deltaBuilder.Len() >= 2048 || time.Since(lastFlush) >= 200*time.Millisecond {
				flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	flush()

	waitErr := session.Wait()
	return finalizeBoundedOutput(outBuilder, maxBytes, tee), waitErr
}

// executeInternalTool executes an internal tool (without running an external command)
func (e *Executor) executeInternalTool(ctx context.Context, toolName string, command string, args map[string]interface{}) (*mcp.ToolResult, error) {
	internalToolType := strings.TrimPrefix(command, "internal:")
	e.logger.Warn("unknown internal tool",
		zap.String("toolName", toolName),
		zap.String("internalToolType", internalToolType),
	)
	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: fmt.Sprintf("Error: unknown internal tool type: %s", internalToolType),
			},
		},
		IsError: true,
	}, nil
}

// buildInputSchema builds the input schema
func (e *Executor) buildInputSchema(toolConfig *config.ToolConfig) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}

	// If the configuration defines parameters, prefer the parameter definitions from the configuration
	if len(toolConfig.Parameters) > 0 {
		properties := make(map[string]interface{})
		required := []string{}

		for _, param := range toolConfig.Parameters {
			// Skip parameters with an empty name (to avoid an invalid schema from name: null or empty in YAML)
			if strings.TrimSpace(param.Name) == "" {
				e.logger.Debug("skipping a parameter with no name",
					zap.String("tool", toolConfig.Name),
					zap.String("type", param.Type),
				)
				continue
			}
			// Convert the type to the OpenAI/JSON Schema standard type (an empty type defaults to string)
			openAIType := e.convertToOpenAIType(param.Type)

			prop := map[string]interface{}{
				"type":        openAIType,
				"description": param.Description,
			}

			// JSON Schema/OpenAI requires the array type to include items, otherwise the API reports invalid_function_parameters
			if openAIType == "array" {
				itemType := strings.TrimSpace(param.ItemType)
				if itemType == "" {
					itemType = "string"
				}
				prop["items"] = map[string]interface{}{
					"type": e.convertToOpenAIType(itemType),
				}
			}

			// Add the default value
			if param.Default != nil {
				prop["default"] = param.Default
			}

			// Add the enum options
			if len(param.Options) > 0 {
				prop["enum"] = param.Options
			}

			properties[param.Name] = prop

			// Add to the list of required parameters
			if param.Required {
				required = append(required, param.Name)
			}
		}

		schema["properties"] = properties
		schema["required"] = required
		return schema
	}

	// If no parameter configuration is defined, return an empty schema
	// In this case the tool may use only fixed arguments (the args field)
	// or parameters may need to be defined via the YAML configuration file
	e.logger.Warn("tool has no parameter configuration defined, returning an empty schema",
		zap.String("tool", toolConfig.Name),
	)
	return schema
}

// convertToOpenAIType converts the configured type to the OpenAI/JSON Schema standard type
func (e *Executor) convertToOpenAIType(configType string) string {
	// Empty or null types are uniformly treated as string, to avoid an invalid schema causing tool-call failures
	if strings.TrimSpace(configType) == "" {
		return "string"
	}
	switch configType {
	case "bool":
		return "boolean"
	case "int", "integer":
		return "number"
	case "float", "double":
		return "number"
	case "string", "array", "object":
		return configType
	default:
		// By default return the original type, but log a warning
		e.logger.Warn("unknown parameter type, using the original type",
			zap.String("type", configType),
		)
		return configType
	}
}

// getExitCode extracts the exit code from an error; returns nil if it is not an ExitError
func getExitCode(err error) *int {
	if err == nil {
		return nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		if exitError.ProcessState != nil {
			exitCode := exitError.ExitCode()
			return &exitCode
		}
	}
	return nil
}

// getExitCodeValue extracts the exit-code value from an error; returns -1 if it is not an ExitError
func getExitCodeValue(err error) int {
	if code := getExitCode(err); code != nil {
		return *code
	}
	return -1
}
