package config

import (
	"strings"
	"testing"
)

func TestDefaultHitlAuditAgentPromptIncludesPrioritizedRules(t *testing.T) {
	prompt := DefaultHitlAuditAgentPrompt()
	for _, want := range []string{
		"if both reject and approve match, it must reject",
		"modify/reset any user or administrator password",
		"modify/create/delete users, roles, permissions",
		"stop, disable, or restart business services",
		"matched rule: ...",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default approval prompt missing %q", want)
		}
	}
}

func TestDefaultHitlAuditAgentPromptReviewEditKeepsEditedArguments(t *testing.T) {
	prompt := DefaultHitlAuditAgentPromptReviewEdit()
	if !strings.Contains(prompt, `"editedArguments":{...}`) {
		t.Fatal("review-edit prompt must preserve editedArguments output")
	}
	if !strings.Contains(prompt, "matched rule: ...") {
		t.Fatal("review-edit prompt must require a matched rule")
	}
}

func TestJevOperatorPolicySkipsDefaultPrompt(t *testing.T) {
	if got := (HitlConfig{}).JevOperatorPolicy("approval"); got != "" {
		t.Fatalf("empty config should not send default prompt to Jev, got %q", got)
	}
	if got := (HitlConfig{AuditAgentPrompt: DefaultHitlAuditAgentPrompt()}).JevOperatorPolicy("approval"); got != "" {
		t.Fatalf("default prompt should not be sent to Jev, got %q", got)
	}
	if got := (HitlConfig{AuditAgentPrompt: "block all command execution"}).JevOperatorPolicy("approval"); got != "block all command execution" {
		t.Fatalf("custom prompt=%q", got)
	}
}
