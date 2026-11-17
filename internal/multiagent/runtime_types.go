package multiagent

type RunResult struct {
	RuntimeStatus           string // immutable runtime outcome; delivery finalization may update Status separately
	RuntimeCompletionReason string
	Response                string
	MCPExecutionIDs         []string
	LastAgentTraceInput     string // serialized message trace (JSON): written by both the native loop and Agent, for resume/attack chain context recovery
	LastAgentTraceOutput    string // this turn's assistant-side display text (summary or final reply)
	Finalized               bool
	Status                  string
	CompletionReason        string
	EvidenceVerified        bool
	EvidenceRefs            []string
	PendingExecutionIDs     []string
	MissingChecks           []string
}
