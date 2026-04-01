package stats

import (
	"strings"
	"testing"
)

func TestParseToolPartData(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    toolPartData
		wantErr     bool
		errContains string
	}{
		{
			name:  "valid tool part with completed status",
			input: `{"type":"tool","tool":"bash","state":{"status":"completed"}}`,
			expected: toolPartData{
				Type: "tool",
				Tool: "bash",
				State: struct {
					Status string `json:"status"`
				}{Status: "completed"},
			},
			wantErr: false,
		},
		{
			name:  "valid tool part with error status",
			input: `{"type":"tool","tool":"read","state":{"status":"error"}}`,
			expected: toolPartData{
				Type: "tool",
				Tool: "read",
				State: struct {
					Status string `json:"status"`
				}{Status: "error"},
			},
			wantErr: false,
		},
		{
			name:  "valid tool part with pending status",
			input: `{"type":"tool","tool":"write","state":{"status":"pending"}}`,
			expected: toolPartData{
				Type: "tool",
				Tool: "write",
				State: struct {
					Status string `json:"status"`
				}{Status: "pending"},
			},
			wantErr: false,
		},
		{
			name:  "non-tool type is valid",
			input: `{"type":"text","state":{"status":"completed"}}`,
			expected: toolPartData{
				Type: "text",
				Tool: "",
				State: struct {
					Status string `json:"status"`
				}{Status: "completed"},
			},
			wantErr: false,
		},
		{
			name:  "missing state defaults to empty",
			input: `{"type":"tool","tool":"bash"}`,
			expected: toolPartData{
				Type: "tool",
				Tool: "bash",
				State: struct {
					Status string `json:"status"`
				}{Status: ""},
			},
			wantErr: false,
		},
		{
			name:        "empty string",
			input:       "",
			wantErr:     true,
			errContains: "empty part data",
		},
		{
			name:        "whitespace only",
			input:       "   \n\t  ",
			wantErr:     true,
			errContains: "empty part data",
		},
		{
			name:        "invalid JSON",
			input:       `{not valid json}`,
			wantErr:     true,
			errContains: "invalid JSON",
		},
		{
			name:        "JSON array instead of object",
			input:       `[{"type":"tool"}]`,
			wantErr:     true,
			errContains: "invalid JSON",
		},
		{
			name:        "missing type",
			input:       `{"tool":"bash","state":{"status":"completed"}}`,
			wantErr:     true,
			errContains: "missing part type",
		},
		{
			name:        "tool type without tool name",
			input:       `{"type":"tool","state":{"status":"completed"}}`,
			wantErr:     true,
			errContains: "missing tool name",
		},
		{
			name:        "tool type with empty tool name",
			input:       `{"type":"tool","tool":"","state":{"status":"completed"}}`,
			wantErr:     true,
			errContains: "missing tool name",
		},
		{
			name:  "extra fields ignored",
			input: `{"type":"tool","tool":"bash","extra":"ignored","state":{"status":"completed"}}`,
			expected: toolPartData{
				Type: "tool",
				Tool: "bash",
				State: struct {
					Status string `json:"status"`
				}{Status: "completed"},
			},
			wantErr: false,
		},
		{
			name:  "null values handled",
			input: `{"type":"tool","tool":"bash","state":null}`,
			expected: toolPartData{
				Type: "tool",
				Tool: "bash",
				State: struct {
					Status string `json:"status"`
				}{Status: ""},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseToolPartData(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseToolPartData(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("parseToolPartData(%q) error = %v, want error containing %q", tt.input, err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("parseToolPartData(%q) unexpected error: %v", tt.input, err)
				return
			}

			if result.Type != tt.expected.Type {
				t.Errorf("Type = %q, want %q", result.Type, tt.expected.Type)
			}
			if result.Tool != tt.expected.Tool {
				t.Errorf("Tool = %q, want %q", result.Tool, tt.expected.Tool)
			}
			if result.State.Status != tt.expected.State.Status {
				t.Errorf("State.Status = %q, want %q", result.State.Status, tt.expected.State.Status)
			}
		})
	}
}

func TestParseToolPartData_WhitespaceTrimming(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "leading whitespace",
			input: "   {\"type\":\"tool\",\"tool\":\"bash\",\"state\":{\"status\":\"completed\"}}",
		},
		{
			name:  "trailing whitespace",
			input: "{\"type\":\"tool\",\"tool\":\"bash\",\"state\":{\"status\":\"completed\"}}   ",
		},
		{
			name:  "leading newline",
			input: "\n{\"type\":\"tool\",\"tool\":\"bash\",\"state\":{\"status\":\"completed\"}}",
		},
		{
			name:  "trailing newline",
			input: "{\"type\":\"tool\",\"tool\":\"bash\",\"state\":{\"status\":\"completed\"}}\n",
		},
		{
			name:  "surrounded by whitespace",
			input: "\t  {\"type\":\"tool\",\"tool\":\"bash\",\"state\":{\"status\":\"completed\"}}  \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseToolPartData(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result.Type != "tool" {
				t.Errorf("Type = %q, want tool", result.Type)
			}
			if result.Tool != "bash" {
				t.Errorf("Tool = %q, want bash", result.Tool)
			}
			if result.State.Status != "completed" {
				t.Errorf("State.Status = %q, want completed", result.State.Status)
			}
		})
	}
}

func TestParseToolPartData_EdgeCases(t *testing.T) {
	t.Run("tool type with various tool names", func(t *testing.T) {
		toolNames := []string{
			"bash",
			"read",
			"write",
			"edit",
			"context7_query-docs",
			"serper_search",
			"tool_with_underscore",
			"tool-with-dash",
			"tool.with.dots",
			"CamelCaseTool",
		}

		for _, toolName := range toolNames {
			input := `{"type":"tool","tool":"` + toolName + `","state":{"status":"completed"}}`
			result, err := parseToolPartData(input)
			if err != nil {
				t.Errorf("tool name %q: unexpected error: %v", toolName, err)
				continue
			}
			if result.Tool != toolName {
				t.Errorf("tool name %q: Tool = %q, want %q", toolName, result.Tool, toolName)
			}
		}
	})

	t.Run("various status values", func(t *testing.T) {
		statuses := []string{
			"completed",
			"error",
			"pending",
			"running",
			"cancelled",
			"", // empty status
		}

		for _, status := range statuses {
			input := `{"type":"tool","tool":"bash","state":{"status":"` + status + `"}}`
			result, err := parseToolPartData(input)
			if err != nil {
				t.Errorf("status %q: unexpected error: %v", status, err)
				continue
			}
			if result.State.Status != status {
				t.Errorf("status %q: State.Status = %q, want %q", status, result.State.Status, status)
			}
		}
	})
}
