package multiagent

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
)

// plan_execute's Replanner / Executor prompt linearly concatenates each step's Result; unbounded, it easily overflows the context.
// This only constrains "the view written into the model prompt", without modifying the original ExecutedSteps in the Eino session.

const (
	defaultPlanExecuteMaxStepResultRunes = 4000
	defaultPlanExecuteKeepLastSteps      = 8
	// Backward-compatible aliases for tests and existing references.
	planExecuteMaxStepResultRunes = defaultPlanExecuteMaxStepResultRunes
	planExecuteKeepLastSteps      = defaultPlanExecuteKeepLastSteps
)

func truncateRunesWithSuffix(s string, maxRunes int, suffix string) string {
	if maxRunes <= 0 || s == "" {
		return s
	}
	rs := []rune(s)
	if len(rs) <= maxRunes {
		return s
	}
	return string(rs[:maxRunes]) + suffix
}

// capPlanExecuteExecutedSteps collapses earlier steps and truncates overly long single-step results, for use in the prompt.
func capPlanExecuteExecutedSteps(steps []planexecute.ExecutedStep) []planexecute.ExecutedStep {
	return capPlanExecuteExecutedStepsWithConfig(steps, nil)
}

func capPlanExecuteExecutedStepsWithConfig(steps []planexecute.ExecutedStep, mwCfg *config.MultiAgentEinoMiddlewareConfig) []planexecute.ExecutedStep {
	if len(steps) == 0 {
		return steps
	}
	maxStepResultRunes := defaultPlanExecuteMaxStepResultRunes
	keepLastSteps := defaultPlanExecuteKeepLastSteps
	if mwCfg != nil {
		maxStepResultRunes = mwCfg.PlanExecuteMaxStepResultRunesEffective()
		keepLastSteps = mwCfg.PlanExecuteKeepLastStepsEffective()
	}
	out := make([]planexecute.ExecutedStep, 0, len(steps)+1)
	start := 0
	if len(steps) > keepLastSteps {
		start = len(steps) - keepLastSteps
		var b strings.Builder
		b.WriteString(fmt.Sprintf("(%d steps already completed above; only step titles are kept here to save context, and the full output is omitted. The following %d steps still keep their body.)\n",
			start, keepLastSteps))
		for i := 0; i < start; i++ {
			b.WriteString(fmt.Sprintf("- %s\n", steps[i].Step))
		}
		out = append(out, planexecute.ExecutedStep{
			Step:   "[Earlier steps — titles only]",
			Result: strings.TrimRight(b.String(), "\n"),
		})
	}
	suffix := "\n…[step result truncated]"
	for i := start; i < len(steps); i++ {
		e := steps[i]
		if utf8.RuneCountInString(e.Result) > maxStepResultRunes {
			e.Result = truncateRunesWithSuffix(e.Result, maxStepResultRunes, suffix)
		}
		out = append(out, e)
	}
	return out
}
