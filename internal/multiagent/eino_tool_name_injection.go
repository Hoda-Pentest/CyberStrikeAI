package multiagent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// injectToolNamesOnlyInstruction prepends a compact tool-name-only section into
// the system instruction so the model can reference current callable names.
// toolSearchMiddlewareActive must be true when prependEinoMiddlewares mounted toolsearch (dynamic tools); do not infer this
// by scanning tool names — tool_search is injected by middleware and is usually absent from the pre-split tools list.
func injectToolNamesOnlyInstruction(ctx context.Context, instruction string, tools []tool.BaseTool, toolSearchMiddlewareActive bool) string {
	names := collectToolNames(ctx, tools)
	if len(names) == 0 {
		return strings.TrimSpace(instruction)
	}
	hasToolSearch := toolSearchMiddlewareActive
	if !hasToolSearch {
		for _, n := range names {
			if strings.EqualFold(strings.TrimSpace(n), "tool_search") {
				hasToolSearch = true
				break
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("The following is the index of tool names bound to the current session (names only, no parameter JSON Schema).\n")
	sb.WriteString("Note: if tool_search is enabled, the list may contain \"non-resident\" tools -- they do not necessarily appear in the tool definitions sent to the model this round; before seeing the tool's full schema, do not guess parameters from the name.\n")
	for _, name := range names {
		sb.WriteString("- ")
		sb.WriteString(name)
		sb.WriteByte('\n')
	}
	sb.WriteString("\nUsage rules:\n")
	sb.WriteString("1) The table above is only a name index, without parameter definitions. Do not guess parameter names, types, enum values, or whether they are required.\n")
	if hasToolSearch {
		sb.WriteString("[Mandatory / highest priority] This session has tool_search enabled (a dynamic tool pool). For any tool that appears in the name index but whose full parameter schema you cannot see in \"the tools definitions attached to the current request\", you must call tool_search first; skipping tool_search to save tokens or rush progress and calling a business tool directly is an explicitly forbidden, incorrect flow.\n")
		sb.WriteString("2) Default policy: whenever you have any uncertainty about a target tool's parameter definition, call tool_search first; prefer an extra tool_search over blindly calling a business tool before seeing the schema.\n")
		sb.WriteString("3) Call order: first tool_search (the only required parameter is regex_pattern: a regex matching the tool name, such as the substring nuclei or ^exact_tool_name$) -> in a later round confirm the target tool appears in the tools list and you have read its schema -> then make the real call to that tool.\n")
		sb.WriteString("4) tool_search returns only the list of matched tool names; the schema is sent in the round after unlocking. Do not fabricate JSON parameters before the schema appears.\n")
		sb.WriteString("5) Do not invent tool names that do not exist.\n\n")
	} else {
		sb.WriteString("2) Before calling a specific tool, confirm the tool's parameter requirements (per the tool definitions in the current request); when unsure, clarify before calling.\n")
		sb.WriteString("3) Do not invent tool names that do not exist.\n\n")
	}
	if s := strings.TrimSpace(injectShellToolGuidance("", names)); s != "" {
		sb.WriteString(s)
		sb.WriteString("\n\n")
	}
	if s := strings.TrimSpace(instruction); s != "" {
		sb.WriteString(s)
	}
	return sb.String()
}

func collectToolNames(ctx context.Context, tools []tool.BaseTool) []string {
	if len(tools) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

