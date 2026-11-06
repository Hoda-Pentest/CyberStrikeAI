package openai

// SSEAccumulatedKey is the field, in an SSE progress event's data, that carries the
// server's authoritative full-text streaming snapshot.
// The frontend should prefer this field to update its buffer, to avoid doubled
// characters from re-normalizing a delta a second time.
const SSEAccumulatedKey = "accumulated"

// WithSSEAccumulated attaches the current streaming accumulated full text (the
// authoritative snapshot) to the progress data.
func WithSSEAccumulated(data map[string]interface{}, accumulated string) map[string]interface{} {
	if data == nil {
		data = make(map[string]interface{}, 1)
	}
	data[SSEAccumulatedKey] = accumulated
	return data
}

// NormalizeStreamingDelta normalizes content that may be an "accumulated chunk /
// resent chunk" into a "pure increment".
// Identical to the unexported normalizeStreamingDelta, exposed for packages such as
// agent / multiagent to accumulate text before sending an SSE event.
func NormalizeStreamingDelta(current, incoming string) (next, delta string) {
	return normalizeStreamingDelta(current, incoming)
}
