package openai

import "testing"

func TestNormalizeStreamingDelta_RepeatedCharBoundary(t *testing.T) {
	// Streaming splits at a repeated-digit boundary: must not merge the first char of "43" with the last char of "194".
	cur, d := normalizeStreamingDelta("https://x:194", "43")
	if want := "https://x:19443"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "43" {
		t.Fatalf("delta: want %q got %q", "43", d)
	}
}

func TestNormalizeStreamingDelta_CumulativePrefix(t *testing.T) {
	cur, d := normalizeStreamingDelta("αβ", "αββγ")
	if cur != "αββγ" || d != "βγ" {
		t.Fatalf("got cur=%q d=%q", cur, d)
	}
}

func TestNormalizeStreamingDelta_FullRetransmit(t *testing.T) {
	cur, d := normalizeStreamingDelta("αβ", "αβ")
	if d != "" || cur != "αβ" {
		t.Fatalf("got cur=%q d=%q", cur, d)
	}
}

func TestNormalizeStreamingDelta_SingleRuneRepeated(t *testing.T) {
	cur, d := normalizeStreamingDelta("δ", "δ")
	if want := "δδ"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "δ" {
		t.Fatalf("delta: want %q got %q", "δ", d)
	}
	cur, d = normalizeStreamingDelta("4", "4")
	if want := "44"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "4" {
		t.Fatalf("delta: want %q got %q", "4", d)
	}
}

func TestNormalizeStreamingDelta_CumulativeExtendsNumber(t *testing.T) {
	// After "194" is buffered, the cumulative string "19443" arrives (note that "1943" is not a prefix of "19443", so a mistyped intermediate state cannot be used to test HasPrefix).
	cur, d := normalizeStreamingDelta("194", "19443")
	if want := "19443"; cur != want {
		t.Fatalf("next: want %q got %q", want, cur)
	}
	if d != "43" {
		t.Fatalf("delta: want %q got %q", "43", d)
	}
}
