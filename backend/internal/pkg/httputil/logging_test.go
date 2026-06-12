package httputil

import (
	"strings"
	"testing"
)

func TestSanitizeHeaderValueForLog(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "trims and collapses whitespace",
			value: "  edge-rid\r\n\t next  ",
			want:  "edge-rid next",
		},
		{
			name:  "drops non-whitespace control characters",
			value: "railway\x00-rid\x7f",
			want:  "railway-rid",
		},
		{
			name:  "returns empty after trimming",
			value: " \r\n\t ",
			want:  "",
		},
		{
			name:  "truncates long values",
			value: strings.Repeat("a", maxLogHeaderValueLen+1),
			want:  strings.Repeat("a", maxLogHeaderValueLen),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeHeaderValueForLog(tt.value); got != tt.want {
				t.Fatalf("SanitizeHeaderValueForLog()=%q, want %q", got, tt.want)
			}
		})
	}
}
