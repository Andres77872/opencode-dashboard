package stats

import (
	"testing"
)

func TestParsePeriod(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int
		wantErr     bool
		errContains string
	}{
		{name: "7d", input: "7d", expected: 7, wantErr: false},
		{name: "30d", input: "30d", expected: 30, wantErr: false},
		{name: "invalid empty", input: "", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid 1d", input: "1d", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid 14d", input: "14d", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid format", input: "seven", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid number only", input: "7", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid day letter", input: "7x", expected: 0, wantErr: true, errContains: "invalid period"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePeriod(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePeriod(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("parsePeriod(%q) error = %q, want error containing %q", tt.input, err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("parsePeriod(%q) unexpected error: %v", tt.input, err)
				return
			}

			if result != tt.expected {
				t.Errorf("parsePeriod(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && containsString(s[1:], substr)
}
