package handler

import (
	"strings"

	"cyberstrike-ai/internal/database"
)

// WebshellSkillHintDefault 对话页 / Agent 单代理共用的 Skills 说明，放在 webshell 上下文末尾，
// 供 AI 选择 skill 加载入口时参考。
const WebshellSkillHintDefault = "Skills 包请使用「多代理 / Agent DeepAgent」会话中的内置 `skill` 工具渐进加载。"

// WebshellSkillHintMultiAgent 多代理 / Agent 多代理准备阶段使用的 Skills 说明
const WebshellSkillHintMultiAgent = "Skills 包请使用 Agent 多代理内置 `skill` 工具。"

// webshellAssistantToolList the tool list the AI assistant is allowed to use in the WebShell context (shown to the model).
// note: this is just a display string; the real permission limit is in the roleTools slice set by the caller.
const webshellAssistantToolList = "webshell_exec, webshell_file_list, webshell_file_read, webshell_file_write, record_vulnerability, list_vulnerabilities, get_vulnerability, upsert_project_fact, get_project_fact, list_project_facts, search_project_facts, deprecate_project_fact, restore_project_fact, list_knowledge_risk_types, search_knowledge_base"

// BuildWebshellAssistantContext assembles the AI assistant's context prompt from the connection info and the user's original message.
// the context includes: connection ID, remark, target system (and the corresponding recommended command set), response encoding, available tool list, Skills loading entry,
// and the final user request. The caller only needs to decide the text of skillHint (default is WebshellSkillHintDefault).
//
// the reason this logic is extracted into a shared function is to avoid copy-pasting it in multiple places such as agent.go / multi_agent_prepare.go,
// and ensure that when we upgrade the OS / Encoding wording, we only need to change one place, test one place, and have it take effect in sync.
func BuildWebshellAssistantContext(conn *database.WebShellConnection, skillHint, userMsg string) string {
	if conn == nil {
		// fallback: the caller already guarantees conn is non-nil; this just defensively returns the original message
		return userMsg
	}
	remark := conn.Remark
	if remark == "" {
		remark = conn.URL
	}

	targetOS := resolveWebshellOS(conn.OS, conn.Type) // normalized to "linux" / "windows"
	encoding := normalizeWebshellEncoding(conn.Encoding)
	if skillHint == "" {
		skillHint = WebshellSkillHintDefault
	}

	var b strings.Builder
	b.Grow(512 + len(userMsg))

	b.WriteString("[WebShell assistant context] Connection ID: ")
	b.WriteString(conn.ID)
	b.WriteString(", Remark: ")
	b.WriteString(remark)
	b.WriteByte('\n')

	// target system: explicitly tell the AI the command set it can/cannot use, to avoid it sending ls/cat/rm to Windows
	b.WriteString("- Target system: ")
	b.WriteString(describeTargetOSForPrompt(targetOS))
	b.WriteByte('\n')

	// response encoding: tell explicitly only when not auto; auto mode is adapted by the backend and does not bother the model
	if encHint := describeEncodingForPrompt(encoding); encHint != "" {
		b.WriteString("- Response encoding: ")
		b.WriteString(encHint)
		b.WriteByte('\n')
	}

	// tool list & connection_id constraint: keep the old wording, the AI is already familiar with it
	b.WriteString("Available tools (use only when operating on this connection, set connection_id to \"")
	b.WriteString(conn.ID)
	b.WriteString("\"): ")
	b.WriteString(webshellAssistantToolList)
	b.WriteString(". Record as you go while pentesting: upsert_project_fact whenever a new finding is confirmed, record_vulnerability whenever a vulnerability is verified; do not wait until the session ends.")
	b.WriteString(skillHint)
	b.WriteString("\n\nUser request: ")
	b.WriteString(userMsg)

	return b.String()
}

// describeTargetOSForPrompt returns the Chinese description + recommended command set + counter-examples for a given OS,
// the command list covers the 6 most common file-management actions (view/read/delete/rename/mkdir/find), so the AI can copy them directly.
func describeTargetOSForPrompt(targetOS string) string {
	switch targetOS {
	case "windows":
		return "Windows (recommended cmd/PowerShell: dir /a, type, del /q /f, move /y, md, ren; " +
			"to find files use `dir /s /b filterword` or PowerShell `Get-ChildItem -Recurse`;" +
			"avoid ls / cat / rm / mv / find and other Unix commands, otherwise it will return `not recognized as an internal or external command`)"
	case "linux":
		return "Linux/Unix (recommended sh/bash: ls -la, cat, rm -f, mv, mkdir -p; " +
			"to find files use `find /path -name '*pattern*'`;" +
			"avoid dir, type, del, move and other Windows commands)"
	default:
		// in theory this will never be reached; resolveWebshellOS already has a fallback
		return "unknown (first run `uname || ver` to probe before deciding the command set)"
	}
}

// describeEncodingForPrompt returns a human-readable description of the response encoding; auto returns an empty string to reduce tokens.
func describeEncodingForPrompt(encoding string) string {
	switch encoding {
	case "utf-8":
		return "UTF-8 (target is natively UTF-8, no extra decoding needed)"
	case "gbk":
		return "GBK (Chinese Windows; the backend has already transcoded to UTF-8 before returning; if many \\uFFFD replacement characters still appear, the command failed or the encoding was misidentified)"
	case "gb18030":
		return "GB18030 (the backend has already transcoded to UTF-8 before returning)"
	default:
		return ""
	}
}
