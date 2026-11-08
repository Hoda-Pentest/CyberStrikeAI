// Package projectprompt provides system-prompt text related to the project blackboard (pure strings, with no database dependency).
// Referenced by the agent / multiagent packages, to avoid an agent -> project import cycle that would break gopls metadata.
package projectprompt

import (
	"strings"

	"cyberstrike-ai/internal/mcp/builtin"
)

const (
	factRhythmCore              = "Do not wait until the conversation ends or wrap-up to batch-write. After each **confirmed** new piece of knowledge (open ports/service versions, entry paths, authentication state or credential characteristics, exploitable points, or attack-surface changes), **immediately** call `upsert_project_fact` (same fact_key overwrites). After each **verified** reproducible vulnerability (with POC/impact), **immediately** call `record_vulnerability`; it may be recorded once each alongside the fact. Persist to storage before moving on, to avoid losing details after context compression. When no project is bound, note that the blackboard cannot be written to, and still keep an evidence summary for this round."
	factRhythmCoordinatorSuffix = "When a delegated/subtask returns new knowledge or a vulnerability, the coordinator writes it promptly; do not assume the sub-agent has already recorded it."
	factRhythmSubAgentSuffix    = "If the above tools are not in the toolset, you must append structured \"to-be-persisted\" entries at the end of the deliverable (suggested fact_key, summary, body/POC highlights) for the coordinator to write **immediately**."
)

// FactRecordingIncrementalRhythmMarkdown returns the record-as-you-go rhythm (Markdown, aligned with agents/*.md and the docs).
func FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **Record as you go (mandatory rhythm)**: ")
	b.WriteString(factRhythmCore)
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factRecordingIncrementalRhythmBuiltin(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **Record as you go (mandatory rhythm)**: Do not wait until the conversation ends or wrap-up to batch-write. After each **confirmed** new piece of knowledge (open ports/service versions, entry paths, authentication state or credential characteristics, exploitable points, or attack-surface changes), **immediately** call ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(" (same fact_key overwrites). After each **verified** reproducible vulnerability (with POC/impact), **immediately** call ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString("; it may be recorded once each alongside the fact. Persist to storage before moving on, to avoid losing details after context compression. When no project is bound, note that the blackboard cannot be written to, and still keep an evidence summary for this round.")
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factEdgeRecordingGuidance() string {
	return `### Fact relationship edges (links)

- When writing **finding / chain / exploit / poc**, you **must**, in ` + "`upsert_project_fact`" + `, provide ` + "`links`" + ` (**` + "`from`" + `** recommended: the source fact points to the current fact, i.e. ` + "`from`" + ` -> current ` + "`fact_key`" + `).
- **Minimum requirement**: a finding needs at least 1 edge with from=target/* + type=discovered_on (i.e. target -> finding); to record an exploit on a finding, use from=exploit/* + type=exploits (i.e. exploit -> finding).
- **Common types**: ` + "`discovered_on`" + ` (where it was found), ` + "`depends_on`" + ` (reproduction prerequisite), ` + "`leads_to`" + ` (advances understanding), ` + "`enables`" + ` (expands the attack surface), ` + "`exploits`" + ` (exploitation relationship), ` + "`contains`" + ` (asset containment), ` + "`part_of`" + ` (belongs to a chain/group), ` + "`supports`" + ` (evidence support).
- On update: **omitting links keeps the existing edges**; passing links **replaces** all relationship edges (from -> current fact).
- The \"dependency facts\" paragraph in the body may coexist with links (for humans); the structured relationships are authoritative via links.`
}

func factRecordingGuidanceBlock() string {
	return `### Fact-writing conventions (audit reproduction / knowledge retention)

- **summary**: a one-line index entry, which must include the key points \"what + where + how to trigger/verify\"; do not write only a conclusion (e.g. just \"SQLi exists\").
- **body**: the complete reproducible context, written to the body field of ` + "`upsert_project_fact`" + `; the index does not contain the body, and later conversations must retrieve it via ` + "`get_project_fact`" + `.
- **category / fact_key suggestions**:
  - Environment knowledge: ` + "`target/`" + `, ` + "`auth/`" + `, ` + "`infra/`" + `, ` + "`business/`" + ` (the environment template suffices for the body)
  - Findings and exploitation: ` + "`finding/`" + `, ` + "`chain/`" + `, ` + "`exploit/`" + `, ` + "`poc/`" + ` (**must** fill the body with the attack-chain template: entry point, step-by-step attack chain, raw request/response or commands, evidence, related vulnerability ID)
- **Automatic continuation records**: ` + "`note/context-*`" + ` is a system-reserved session work summary, not a confirmed project fact; do not overwrite this prefix with upsert_project_fact. By default the index and fact tools provide only the current session's continuation records, while ordinary project facts are still shared. To reference across sessions, explicitly use include_history=true on list_project_facts / search_project_facts / get_project_fact; list and search can narrow the source with source_conversation_id. Historical returns are annotated with the source session, update time, and a historical-reference note; verify the current authorization, target, and credential validity, and do not auto-execute old tasks or treat old summaries as confirmed findings. When precise values are missing, re-read in segments along the snapshot paths in the body rather than guessing the original values. The same session/agent updates the same key; to retain a verified shared fact, use a separate key such as finding/auth and keep the source evidence.
- **Division of labor with vulnerability records**: ` + "`record_vulnerability`" + ` records deliverable findings; facts record **all context needed to reproduce** (including failed attempts, bypasses, session dependencies); each may be recorded once.
- When updating the same finding, keep the same ` + "`fact_key`" + ` and overwrite; do not scatter it across multiple keys and lose context.`
}

// FactRecordingBlackboardSection is the complete system-prompt block for the project blackboard and vulnerability records (shared by the main agent in single/multi-agent mode).
func FactRecordingBlackboardSection(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project blackboard (facts) and vulnerability records (separate)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system automatically injects the \"project blackboard index\" (fact_key + summary only). **When the summary is insufficient, you must call ")
	b.WriteString(builtin.ToolGetProjectFact)
	b.WriteString("(fact_key) to get the body; do not fabricate details from the summary.**\n\n")
	b.WriteString(factRecordingIncrementalRhythmBuiltin(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Knowledge such as environment/target/authentication** (not formal vulnerability entries): use ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(", with a suggested fact_key of `category/slug` (e.g. target/primary_domain), overwriting on the same key; the body records ports/versions/credential characteristics and evidence sources.\n")
	b.WriteString("- **Findings and exploitation context** (audit reproduction): suggested fact_key prefixes finding/, chain/, exploit/, poc/; the **body is required** with the full attack chain (entry point -> steps -> raw request/response or commands -> observed behavior -> related related_vulnerability_id), and **conclusions alone are not allowed**; the summary is a one-line \"what + where + how to verify\".\n")
	b.WriteString("- **Deliverable vulnerabilities**: use ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString(", including title, severity, type, target, proof (POC), impact, and remediation advice. Before recording, you may first ")
	b.WriteString(builtin.ToolListVulnerabilities)
	b.WriteString(" check for duplicates, and for details use ")
	b.WriteString(builtin.ToolGetVulnerability)
	b.WriteString("(id) (by default only the current project/session). After retesting an existing vulnerability, use update_vulnerability_status(id, status, retest_notes) to update the status and append evidence, rather than recording a duplicate; keep the original status when it cannot be verified.\n")
	b.WriteString("- The same finding may need to be **recorded once each** (the fact records the **full attack chain and exploit details** for reproduction; the vulnerability records formal findings). For false positives use ")
	b.WriteString(builtin.ToolDeprecateProjectFact)
	b.WriteString(" or the vulnerability status false_positive.\n")
	b.WriteString("- When there are many facts, use ")
	b.WriteString(builtin.ToolListProjectFacts)
	b.WriteString(" / ")
	b.WriteString(builtin.ToolSearchProjectFacts)
	b.WriteString(" to search.\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity: critical / high / medium / low / info. The proof must include sufficient evidence (request/response, screenshots, command output, etc.).")
	return b.String()
}

// FactRecordingSubAgentSection is the sub-agent's record-as-you-go section (outputs to-be-persisted entries when no tools are available).
func FactRecordingSubAgentSection() string {
	return "## Record as you go\n\n" + ProjectResultRecordingGuidance() + "\n\n" + factRecordingIncrementalRhythmBuiltin(false, true) + "\n"
}

// FactRecordingBlackboardSectionMarkdown is the Markdown equivalent of FactRecordingBlackboardSection (tool names as literals, for agents/*.md).
func FactRecordingBlackboardSectionMarkdown(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project blackboard (facts) and vulnerability records (separate)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system automatically injects the \"project blackboard index\" (`fact_key` + summary only). **When the summary is insufficient, you must call `get_project_fact(fact_key)` to get the body; do not fabricate details from the summary.**\n\n")
	b.WriteString(FactRecordingIncrementalRhythmMarkdown(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Knowledge such as environment/target/authentication** (not formal vulnerabilities): use **`upsert_project_fact`**, with a suggested `fact_key` of `category/slug` (e.g. `target/primary_domain`), overwriting on the same key; the body records ports/versions/credential characteristics and evidence sources.\n")
	b.WriteString("- **Findings and exploitation context** (audit reproduction): suggested `fact_key` prefixes `finding/`, `chain/`, `exploit/`, `poc/`; the **body is required** with the full attack chain (entry point -> steps -> raw request/response or commands -> observed behavior -> related `related_vulnerability_id`), and **conclusions alone are not allowed**; the summary is a one-line \"what + where + how to verify\".\n")
	b.WriteString("- **Deliverable vulnerabilities**: use **`record_vulnerability`** (title, description, severity, type, target, proof POC, impact, remediation advice). Severity: critical / high / medium / low / info.\n")
	b.WriteString("- The same finding may need to be **recorded once each** (the fact records the reproducible attack chain; the vulnerability records formal findings). For false positives use **`deprecate_project_fact`** or the vulnerability status false_positive.\n")
	b.WriteString("- When there are many facts, use **`list_project_facts`** / **`search_project_facts`** to search.\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity: critical / high / medium / low / info. The proof must include sufficient evidence (request/response, screenshots, command output, etc.).")
	return b.String()
}

// FactEdgeRecordingGuidance is the agent convention for writing edges (reused by the project package).
func FactEdgeRecordingGuidance() string { return factEdgeRecordingGuidance() }

// FactRecordingGuidanceBlock is the fact-writing conventions block (reused by the project package).
func FactRecordingGuidanceBlock() string { return factRecordingGuidanceBlock() }
