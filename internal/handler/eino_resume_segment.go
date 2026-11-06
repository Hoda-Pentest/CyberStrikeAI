package handler

import (
	"context"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/multiagent"
)

// applyEinoTraceResumeSegment interrupt and resume: persist last_react_* → loadHistory, optionally replace the next segment of user text.
func (h *AgentHandler) applyEinoTraceResumeSegment(
	conversationID string,
	result *multiagent.RunResult,
	curHistory *[]agent.ChatMessage,
	curFinalMessage *string,
	segmentUserMessage string,
) {
	if shouldPersistEinoAgentTraceAfterRunError(context.Background()) {
		h.persistEinoAgentTraceForResume(conversationID, result)
	}
	if hist, err := h.loadHistoryFromAgentTrace(conversationID); err == nil && len(hist) > 0 {
		*curHistory = hist
	}
	if segmentUserMessage != "" {
		*curFinalMessage = segmentUserMessage
	}
}
