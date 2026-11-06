package database

import (
	"encoding/json"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/mcp"
)

// The legacy envelope was persisted in Chinese by older versions, so the
// migration must keep matching those exact stored strings. They are written as
// \u escapes to keep the source ASCII-only without changing the matched bytes.
const (
	// "Tool call blocked by security rule"
	legacyToolGuardPrefix = "\u5de5\u5177\u8c03\u7528\u5df2\u88ab\u5b89\u5168\u89c4\u5219\u62e6\u622a"
	// Fullwidth colon that follows the prefix in the single-line form.
	legacyToolGuardColon = "\uff1a"
	// "\nRule: "
	legacyToolGuardRuleLabel = "\n\u89c4\u5219: "
	// "\nMatched content: "
	legacyToolGuardMatchLabel = "\n\u5339\u914d\u5185\u5bb9: "
)

// Only the exact envelope emitted by the old local guard is recognized here.
// New executions use the structured marker and never infer policy from text.
func isLegacyToolGuardRefusal(text string) bool {
	if !strings.HasPrefix(text, legacyToolGuardPrefix+legacyToolGuardColon) && !strings.HasPrefix(text, legacyToolGuardPrefix+legacyToolGuardRuleLabel) {
		return false
	}
	matchIndex := strings.LastIndex(text, legacyToolGuardMatchLabel)
	if matchIndex < 0 {
		return false
	}
	if _, err := strconv.Unquote(text[matchIndex+len(legacyToolGuardMatchLabel):]); err != nil {
		return false
	}
	ruleIndex := strings.LastIndex(text[:matchIndex], legacyToolGuardRuleLabel)
	if ruleIndex < 0 {
		return false
	}
	rule := text[ruleIndex+len(legacyToolGuardRuleLabel) : matchIndex]
	idIndex := strings.LastIndex(rule, " (")
	return idIndex > 0 && strings.HasSuffix(rule, ")") && len(rule[idIndex+2:len(rule)-1]) > 0 && !strings.Contains(rule, "\n")
}

// migrateLegacyToolGuardBlocks is idempotent because only failed records qualify.
// Keeping status and accumulated failure counts in one transaction makes monitor
// filters, badges and statistics agree immediately after upgrading.
func (db *DB) migrateLegacyToolGuardBlocks() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, tool_name, error, COALESCE(result, '') FROM tool_executions WHERE status = 'failed' AND error LIKE ?`, legacyToolGuardPrefix+"%")
	if err != nil {
		return err
	}
	type record struct{ id, tool, reason, result string }
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.tool, &r.reason, &r.result); err != nil {
			rows.Close()
			return err
		}
		if isLegacyToolGuardRefusal(r.reason) {
			records = append(records, r)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range records {
		var result mcp.ToolResult
		_ = json.Unmarshal([]byte(r.result), &result)
		if len(result.Content) == 0 {
			result.Content = []mcp.Content{{Type: "text", Text: r.reason}}
		}
		result.Blocked, result.IsError = true, true
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE tool_executions SET status = 'blocked', result = ? WHERE id = ?`, string(encoded), r.id); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE tool_stats SET failed_calls = MAX(0, failed_calls - 1) WHERE tool_name = ?`, r.tool); err != nil {
			return err
		}
	}
	return tx.Commit()
}
