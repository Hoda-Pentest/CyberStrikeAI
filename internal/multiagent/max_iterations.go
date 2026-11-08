package multiagent

import "cyberstrike-ai/internal/config"

const defaultAgentMaxIterations = 3000

// agentMaxIterations is the global cap: it uses only config.agent.max_iterations; <=0 defaults to 3000, consistent with config.
func agentMaxIterations(appCfg *config.Config) int {
	if appCfg != nil && appCfg.Agent.MaxIterations > 0 {
		return appCfg.Agent.MaxIterations
	}
	return defaultAgentMaxIterations
}

// resolveMaxIterations unifies the iteration cap: max_iterations>0 in the Markdown/sub-agent front matter can override it individually, otherwise agent.max_iterations is used.
// multi_agent.max_iteration and sub_agent_max_iterations are deprecated and no longer participate in the calculation.
func resolveMaxIterations(appCfg *config.Config, markdownOverride int) int {
	if markdownOverride > 0 {
		return markdownOverride
	}
	return agentMaxIterations(appCfg)
}
