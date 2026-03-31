package tui

import (
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestFormatInt(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{name: "zero", input: 0, expected: "0"},
		{name: "single digit", input: 5, expected: "5"},
		{name: "two digits", input: 42, expected: "42"},
		{name: "three digits", input: 999, expected: "999"},
		{name: "one thousand", input: 1000, expected: "1,000"},
		{name: "ten thousand", input: 10000, expected: "10,000"},
		{name: "one million", input: 1000000, expected: "1,000,000"},
		{name: "one billion", input: 1000000000, expected: "1,000,000,000"},
		{name: "mixed digits", input: 1234567, expected: "1,234,567"},
		{name: "negative", input: -1234, expected: "-1,234"},
		{name: "negative large", input: -1234567890, expected: "-1,234,567,890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatInt(tt.input)
			if result != tt.expected {
				t.Errorf("formatInt(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatCompactInt(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{name: "zero", input: 0, expected: "0"},
		{name: "hundreds", input: 999, expected: "999"},
		{name: "one thousand exact", input: 1000, expected: "1k"},
		{name: "one thousand decimal", input: 1500, expected: "1.5k"},
		{name: "ten thousand", input: 10000, expected: "10k"},
		{name: "hundred thousand", input: 100000, expected: "100k"},
		{name: "one million exact", input: 1000000, expected: "1m"},
		{name: "one million decimal", input: 2500000, expected: "2.5m"},
		{name: "ten million", input: 10000000, expected: "10m"},
		{name: "one billion exact", input: 1000000000, expected: "1b"},
		{name: "one billion decimal", input: 3500000000, expected: "3.5b"},
		{name: "negative thousand", input: -2500, expected: "-2.5k"},
		{name: "negative million", input: -5000000, expected: "-5m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCompactInt(tt.input)
			if result != tt.expected {
				t.Errorf("formatCompactInt(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatMoney(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected string
	}{
		{name: "zero", input: 0, expected: "$0.00"},
		{name: "small decimal", input: 0.01, expected: "$0.01"},
		{name: "one dollar", input: 1, expected: "$1.00"},
		{name: "cents", input: 1.5, expected: "$1.50"},
		{name: "tens", input: 10.99, expected: "$10.99"},
		{name: "hundreds", input: 123.45, expected: "$123.45"},
		{name: "thousands", input: 1234.56, expected: "$1234.56"},
		{name: "large", input: 12345.67, expected: "$12345.67"},
		{name: "negative", input: -10.5, expected: "$-10.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMoney(tt.input)
			if result != tt.expected {
				t.Errorf("formatMoney(%f) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    stats.TokenStats
		expected string
	}{
		{
			name:     "zero tokens",
			input:    stats.TokenStats{},
			expected: "0 in • 0 out • 0 reason",
		},
		{
			name: "simple tokens",
			input: stats.TokenStats{
				Input:     1000,
				Output:    500,
				Reasoning: 200,
			},
			expected: "1,000 in • 500 out • 200 reason",
		},
		{
			name: "large tokens",
			input: stats.TokenStats{
				Input:     1234567,
				Output:    987654,
				Reasoning: 111111,
			},
			expected: "1,234,567 in • 987,654 out • 111,111 reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTokens(tt.input)
			if result != tt.expected {
				t.Errorf("formatTokens() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAsciiBar(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		maxValue float64
		width    int
		expected string
	}{
		{name: "zero value", value: 0, maxValue: 100, width: 10, expected: ""},
		{name: "zero max", value: 50, maxValue: 0, width: 10, expected: ""},
		{name: "negative value", value: -5, maxValue: 100, width: 10, expected: ""},
		{name: "negative max", value: 50, maxValue: -10, width: 10, expected: ""},
		{name: "zero width", value: 50, maxValue: 100, width: 0, expected: ""},
		{name: "full bar", value: 100, maxValue: 100, width: 5, expected: "█████"},
		{name: "half bar", value: 50, maxValue: 100, width: 10, expected: "█████"},
		{name: "quarter bar", value: 25, maxValue: 100, width: 8, expected: "██"},
		{name: "exceeds max", value: 150, maxValue: 100, width: 5, expected: "█████"},
		{name: "small fraction", value: 1, maxValue: 100, width: 10, expected: "█"},
		{name: "single char width", value: 50, maxValue: 100, width: 1, expected: "█"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := asciiBar(tt.value, tt.maxValue, tt.width)
			if result != tt.expected {
				t.Errorf("asciiBar(%f, %f, %d) = %q, want %q", tt.value, tt.maxValue, tt.width, result, tt.expected)
			}
		})
	}
}

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		expected string
	}{
		{name: "empty string", input: "", width: 10, expected: ""},
		{name: "fits exactly", input: "hello", width: 5, expected: "hello"},
		{name: "fits with room", input: "hello", width: 10, expected: "hello"},
		{name: "truncated", input: "hello world", width: 8, expected: "hello w…"},
		{name: "width equals length minus one", input: "hello", width: 4, expected: "hel…"},
		{name: "width one", input: "hello", width: 1, expected: "…"},
		{name: "width zero", input: "hello", width: 0, expected: ""},
		{name: "negative width", input: "hello", width: -5, expected: ""},
		{name: "unicode chars", input: "héllo wörld", width: 8, expected: "héllo w…"},
		{name: "emoji fits", input: "hello 🌍", width: 8, expected: "hello 🌍"},             // 7 runes, fits in 8
		{name: "emoji truncated", input: "hello 🌍 world", width: 8, expected: "hello 🌍…"}, // 9 runes, truncates
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateWithEllipsis(tt.input, tt.width)
			if result != tt.expected {
				t.Errorf("truncateWithEllipsis(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
			}
		})
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		minV     int
		maxV     int
		expected int
	}{
		{name: "within range", value: 5, minV: 0, maxV: 10, expected: 5},
		{name: "below min", value: -5, minV: 0, maxV: 10, expected: 0},
		{name: "above max", value: 15, minV: 0, maxV: 10, expected: 10},
		{name: "at min", value: 0, minV: 0, maxV: 10, expected: 0},
		{name: "at max", value: 10, minV: 0, maxV: 10, expected: 10},
		{name: "negative range", value: -15, minV: -20, maxV: -10, expected: -15},
		{name: "below negative min", value: -25, minV: -20, maxV: -10, expected: -20},
		{name: "above negative max", value: -5, minV: -20, maxV: -10, expected: -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clamp(tt.value, tt.minV, tt.maxV)
			if result != tt.expected {
				t.Errorf("clamp(%d, %d, %d) = %d, want %d", tt.value, tt.minV, tt.maxV, result, tt.expected)
			}
		})
	}
}

func TestMinInt(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{name: "a smaller", a: 1, b: 10, expected: 1},
		{name: "b smaller", a: 10, b: 1, expected: 1},
		{name: "equal", a: 5, b: 5, expected: 5},
		{name: "negative a", a: -10, b: 5, expected: -10},
		{name: "both negative", a: -10, b: -5, expected: -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := minInt(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("minInt(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMaxInt(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{name: "a larger", a: 10, b: 1, expected: 10},
		{name: "b larger", a: 1, b: 10, expected: 10},
		{name: "equal", a: 5, b: 5, expected: 5},
		{name: "negative a", a: -10, b: 5, expected: 5},
		{name: "both negative", a: -10, b: -5, expected: -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maxInt(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("maxInt(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
