package handler

import (
	"testing"
)

func TestSanitizeDigits(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"1234", 10, "1234"},
		{"12 34", 10, "1234"},
		{"12-34", 10, "1234"},
		{"abc123", 10, "123"},
		{"1234567890", 4, "1234"},
		{"", 10, ""},
		{"!@#$%", 10, ""},
		{"123456789012345", 10, "1234567890"},
		{"1234", 0, ""},   // zero maxLen returns empty
	}
	for _, tt := range tests {
		got := sanitizeDigits(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("sanitizeDigits(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
