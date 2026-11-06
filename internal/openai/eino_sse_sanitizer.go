package openai

// eino_sse_sanitizer.go addresses a problem that occurs when Eino goes through the
// meguminnnnnnnnn/go-openai SDK: a relay's heartbeat/SSE control lines, once they
// accumulate past 300 lines, trigger ErrTooManyEmptyStreamMessages
// (error text: "stream has sent too many empty messages").
//
// Trigger path:
//   einoopenai.NewChatModel
//     -> eino-ext/libs/acl/openai -> meguminnnnnnnnn/go-openai
//     -> streamReader.processLines() counts every non-"data:" line, and throws past 300.
//
// Non-data: lines common on relays (valid SSE, but the SDK rejects them):
//   ":" / ": keepalive" / ": ping" / "event: ping" / "retry: 3000"
//   as well as the heavy heartbeats interleaved during a reasoning model's prefill.
//
// Fallback strategy: wrap the response Body in a reader at the HTTP transport layer
// that only lets lines starting with "data:" through, swallowing heartbeat/comment/
// event-type lines in place. The downstream SDK never sees a non-data: line, so the
// counter always stays at 0 and this error can no longer occur.
//
// This layer is completely transparent to the caller:
//   - It only kicks in when the response Content-Type is text/event-stream; ordinary
//     JSON responses pass through unchanged
//   - The data: payload only backfills the DeepSeek cache usage fields; everything
//     else passes through unchanged
//   - A genuine upstream stream break (EOF / connection reset / context cancel) passes
//     through unchanged

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
)

const (
	// einoSSEReaderBufSize gives bufio a larger initial buffer, to avoid a large
	// single-line JSON chunk (holding tool-call arguments / reasoning_content)
	// repeatedly triggering buffer growth.
	einoSSEReaderBufSize = 64 * 1024
)

// einoSSESanitizingRoundTripper wraps the downstream RoundTripper, sanitizing SSE responses line by line.
type einoSSESanitizingRoundTripper struct {
	base http.RoundTripper
}

func (rt *einoSSESanitizingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if !isSSEResponse(resp) {
		return resp, nil
	}
	resp.Body = newEinoSSESanitizingBody(resp.Body)
	return resp, nil
}

// isSSEResponse only sanitizes 200 + text/event-stream responses;
// error responses are handled by a separate einoSSEErrorRoundTripper, untouched by this layer.
func isSSEResponse(resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		return false
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		return false
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	// Compatible with "text/event-stream", "text/event-stream; charset=utf-8", and so on.
	return strings.HasPrefix(ct, "text/event-stream")
}

// einoSSESanitizingBody is the wrapped response body: it only lets data: lines through, swallowing all others.
type einoSSESanitizingBody struct {
	upstream io.ReadCloser
	reader   *bufio.Reader
	pending  []byte // sanitized bytes awaiting return downstream (always a complete data: line ending in \n)
	err      error  // upstream's terminal error (io.EOF or a network error)
}

func newEinoSSESanitizingBody(body io.ReadCloser) *einoSSESanitizingBody {
	return &einoSSESanitizingBody{
		upstream: body,
		reader:   bufio.NewReaderSize(body, einoSSEReaderBufSize),
	}
}

func (b *einoSSESanitizingBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}

	// Read from upstream until a full data: line accumulates or a terminal state is
	// reached. A single loop may discard any number of heartbeat lines, but it exits
	// after letting through at most one data: line, to avoid a single Read blocking
	// too long or the pending buffer growing too large.
	for b.err == nil {
		line, err := b.reader.ReadBytes('\n')
		if len(line) > 0 {
			if isPassThroughSSELine(line) {
				if line[len(line)-1] != '\n' {
					line = append(line, '\n')
				}
				payload := bytes.TrimSpace(bytes.TrimLeft(line, " \t")[5:])
				normalized := normalizeEinoUsagePayload(payload)
				if !bytes.Equal(payload, normalized) {
					line = append(append([]byte("data: "), normalized...), '\n')
				}
				b.pending = line
				if err != nil {
					b.err = err
				}
				break
			}
			// A non-data: line (blank line / ":" comment / event: / retry: / id: /
			// any bare text) is swallowed entirely, never passed downstream; continue
			// the loop to read the next line.
		}
		if err != nil {
			b.err = err
			break
		}
	}

	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}
	return 0, b.err
}

func (b *einoSSESanitizingBody) Close() error {
	return b.upstream.Close()
}

// isPassThroughSSELine decides whether this line needs to pass through to the
// downstream SDK unchanged. Only a line starting with "data:" (case-insensitive,
// with any amount of leading whitespace) needs to be kept.
// Note: do not TrimSpace off the trailing newline before judging, or "  data: x"
// would be misjudged; we only trim leading whitespace, matching the semantics of the
// SDK's own TrimSpace followed by the regex ^data:\s*.
func isPassThroughSSELine(line []byte) bool {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(trimmed) < 5 {
		return false
	}
	// Case-insensitive comparison of whether the first 5 bytes are "data:". The SSE
	// spec requires lowercase field names, but a lenient match stays compatible with
	// the odd non-compliant relay implementation.
	return (trimmed[0] == 'd' || trimmed[0] == 'D') &&
		(trimmed[1] == 'a' || trimmed[1] == 'A') &&
		(trimmed[2] == 't' || trimmed[2] == 'T') &&
		(trimmed[3] == 'a' || trimmed[3] == 'A') &&
		trimmed[4] == ':'
}
