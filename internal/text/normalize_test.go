package text

import (
	"testing"
)

func TestNormalizeTerms_Basic(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{
			input: "Hello World",
			want:  []string{"hello", "world"},
		},
		{
			input: "The consumer is configured",
			want:  []string{"consumer", "configured"},
		},
		{
			input: "Using the example following",
			want:  []string{}, // All stopwords
		},
		{
			input: "",
			want:  []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeTerms(tc.input)
			if len(got) != len(tc.want) {
				t.Errorf("NormalizeTerms(%q) = %v, want %v", tc.input, got, tc.want)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("NormalizeTerms(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeTerms_FiltersStopwords(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog"
	got := NormalizeTerms(input)

	// Should filter: "the", "over"
	for _, term := range got {
		if IsStopword(term) {
			t.Errorf("Stopword %q should have been filtered", term)
		}
	}

	// Should keep: "quick", "brown", "fox", "jumps", "lazy", "dog"
	expected := []string{"quick", "brown", "fox", "jumps", "lazy", "dog"}
	if len(got) != len(expected) {
		t.Errorf("Got %d terms, want %d: %v", len(got), len(expected), got)
	}
}

// --- Benchmarks ---

// BenchmarkNormalizeTerms_Short measures performance on typical short text.
func BenchmarkNormalizeTerms_Short(b *testing.B) {
	input := "The consumer is configured with these options"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeTerms(input)
	}
}

// BenchmarkNormalizeTerms_Long measures performance on longer documentation text.
func BenchmarkNormalizeTerms_Long(b *testing.B) {
	input := `NATS JetStream is a persistence layer for NATS that provides streaming,
message replay, and exactly-once semantics. A consumer is a stateful view of
a stream. It tracks which messages have been delivered and acknowledged.
Durable consumers persist their state across restarts. They are identified
by a unique name within the stream. Ephemeral consumers are automatically
cleaned up when there are no active subscriptions for a configured period.`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeTerms(input)
	}
}

// BenchmarkNormalizeTerms_WithHTML measures performance when HTML stripping is needed.
func BenchmarkNormalizeTerms_WithHTML(b *testing.B) {
	input := `<p>The <a href="http://example.com">consumer</a> is configured with
<code>AckPolicy</code> and other &amp; options.</p>`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeTerms(input)
	}
}

// BenchmarkStripHTML measures HTML tag and entity removal.
func BenchmarkStripHTML(b *testing.B) {
	input := `<div class="docs"><p>Hello <strong>world</strong> &amp; friends!</p></div>`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = StripHTML(input)
	}
}

// BenchmarkIsStopword measures stopword lookup performance.
func BenchmarkIsStopword(b *testing.B) {
	words := []string{"the", "consumer", "is", "configured", "with"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, w := range words {
			_ = IsStopword(w)
		}
	}
}
