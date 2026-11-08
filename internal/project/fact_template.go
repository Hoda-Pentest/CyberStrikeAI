package project

import (
	"fmt"
	"strings"

	"cyberstrike-ai/internal/projectprompt"
)

// Fact category constants (the category field written by upsert_project_fact).
const (
	FactCategoryTarget   = "target"
	FactCategoryAuth     = "auth"
	FactCategoryInfra    = "infra"
	FactCategoryBusiness = "business"
	FactCategoryFinding  = "finding"
	FactCategoryChain    = "chain"
	FactCategoryExploit  = "exploit"
	FactCategoryPOC      = "poc"
	FactCategoryNote     = "note"
)

// RequiresAttackChainBody decides whether a fact should carry reproducible attack-chain / exploit details (in the body, not just the summary).
func RequiresAttackChainBody(category, factKey string) bool {
	c := strings.ToLower(strings.TrimSpace(category))
	switch c {
	case FactCategoryFinding, FactCategoryChain, FactCategoryExploit, FactCategoryPOC, "vuln":
		return true
	}
	key := strings.ToLower(strings.TrimSpace(factKey))
	for _, prefix := range []string{"finding/", "chain/", "exploit/", "poc/"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// IsSparseFactBody returns true when an attack-chain fact's body is too short or missing key sections (a soft check that does not block writing).
func IsSparseFactBody(category, factKey, body string) bool {
	if !RequiresAttackChainBody(category, factKey) {
		return false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return true
	}
	lower := strings.ToLower(body)
	// It should contain at least one reproducible clue: steps/request/command/code block
	hasSteps := strings.Contains(lower, "attack chain") || strings.Contains(lower, "## attack") ||
		strings.Contains(lower, "## exploit") || strings.Contains(lower, "## poc")
	hasHTTP := strings.Contains(lower, "```http") || strings.Contains(lower, "```bash") ||
		strings.Contains(lower, "curl ") || strings.Contains(lower, "get ") || strings.Contains(lower, "post ")
	hasReq := strings.Contains(lower, "request") || strings.Contains(lower, "response") || strings.Contains(lower, "payload")
	// With no structural clues such as attack chain/POC/request, treat it as a conclusion-only description (regardless of length)
	return !(hasSteps || hasHTTP || hasReq)
}

// FactBodyTemplate returns a suggested body Markdown skeleton by category (for the agent to fill in real content).
func FactBodyTemplate(category, factKey string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(factKey)), "artifact/") {
		return artifactFactBodyTemplate
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(factKey)), "test/") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(factKey)), "negative/") {
		return testResultFactBodyTemplate
	}
	if RequiresAttackChainBody(category, factKey) {
		return attackChainFactBodyTemplate
	}
	return envFactBodyTemplate
}

const attackChainFactBodyTemplate = `## Conclusion (verifiable, one sentence)
<do not write only "a vulnerability exists"; state the type + location + trigger condition>

## Target and entry point
- Target: <URL / IP:Port / hostname>
- Entry point: <path / interface / parameter>
- Preconditions: <anonymous / role / Cookie / other dependencies>

## Attack chain (step-by-step reproducible)
1. <reconnaissance/discovery>
2. <exploitation/trigger>
3. <impact proof (file read, RCE echo, unauthorized data, etc.)>

## Exploit / POC
### Request
` + "```http\n<METHOD> <path> HTTP/1.1\nHost: ...\n...\n\n<body>\n```" + `

### Response / Observation
<key response fragments, status codes, differences>

### Command / script (if any)
` + "```bash\n<command>\n```" + `

## Key evidence
- <tool-output summary / screenshot path / session or message ID>

## Related
- related_vulnerability_id: <optional, the id from the corresponding record_vulnerability>
- links (upsert parameter): [{ "from": "<fact_key>", "type": "discovered_on|..." }] (from -> current fact)
- Dependency facts (human-readable mirror in body): <fact_key, e.g. auth/session_cookie>

## Notes and uncertainties
<assumptions to verify, environment differences, bypass-attempt records>`

const envFactBodyTemplate = `## Summary
<the core knowledge of this fact>

## Details
<ports/versions/paths/credential characteristics/business rules, etc.>

## Source and evidence
<command output, response fragments, discovery time>

## Related
- Related fact_key: <optional>`

// FactRecordingGuidanceBlock is written into the system prompt: it requires facts to retain attack-chain context rather than only a conclusion.
func FactRecordingGuidanceBlock() string {
	return projectprompt.FactRecordingGuidanceBlock()
}

// SparseBodyWarning is the tool-return hint when an attack-chain fact's body is insufficient (does not block saving).
func SparseBodyWarning(category, factKey string) string {
	if !IsSparseFactBody(category, factKey, "") {
		return ""
	}
	return fmt.Sprintf(
		"\n\n⚠ Hint: category=%q / fact_key=%q is an attack-chain fact, but the body is empty or too simple. Please add the full attack chain and POC (refer to the template) so it can be reproduced in later audits.\nSuggested body skeleton:\n%s",
		category, factKey, FactBodyTemplate(category, factKey),
	)
}

// SparseBodyWarningIfNeeded decides whether to append the warning based on the actual body.
func SparseBodyWarningIfNeeded(category, factKey, body string) string {
	if !IsSparseFactBody(category, factKey, body) {
		return ""
	}
	return SparseBodyWarning(category, factKey)
}

const artifactFactBodyTemplate = `## Result and verification status
<purpose, applicable target/entry; verified successful / failed / not verified>

## File and invocation
- Path relative to the project working directory: artifacts/<file name>
- Dependencies and versions: <dependency files, install/run environment>
- Invocation and variable parameters: <commands, inputs/outputs; reference credentials via auth/*>

## Verification evidence
<verification time, raw commands/output, applicable conditions; do not infer success from a file's existence>

## Related
<source session, dependency fact_key, related_vulnerability_id>`

const testResultFactBodyTemplate = `## Tested conditions
<target, entry, parameters, identity/authentication, version, test time>

## Method and evidence
<request/command, response differences, tool output; errors/timeouts must be noted separately>

## Conclusion and boundaries
<success / not found under tested conditions / blocked / to be verified; do not extend to untested entries>

## Retest trigger conditions
<changes to target/entry/identity/version, insufficient original evidence, or a user request to retest>

## Related
<source session, script artifact/*, related fact/vulnerability IDs>`
