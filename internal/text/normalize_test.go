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
		if _, isStop := Stopwords[term]; isStop {
			t.Errorf("Stopword %q should have been filtered", term)
		}
	}

	// Should keep: "quick", "brown", "fox", "jumps", "lazy", "dog"
	expected := []string{"quick", "brown", "fox", "jumps", "lazy", "dog"}
	if len(got) != len(expected) {
		t.Errorf("Got %d terms, want %d: %v", len(got), len(expected), got)
	}
}
