package printer

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		outputType OutputType
		wide       bool
	}{
		{"Table output", OutputTypeTable, false},
		{"Wide output", OutputTypeWide, true},
		{"JSON output", OutputTypeJSON, false},
		{"YAML output", OutputTypeYAML, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.outputType, tt.wide)

			if p == nil {
				t.Fatal("New() returned nil")
			}

			if p.outputType != tt.outputType {
				t.Errorf("Expected outputType %s, got %s", tt.outputType, p.outputType)
			}

			if p.wide != tt.wide {
				t.Errorf("Expected wide %v, got %v", tt.wide, p.wide)
			}

			if p.out == nil {
				t.Error("Output writer is nil")
			}
		})
	}
}

func TestSetOutput(t *testing.T) {
	p := New(OutputTypeTable, false)
	buf := &bytes.Buffer{}

	p.SetOutput(buf)

	if p.out != buf {
		t.Error("SetOutput() did not set the output writer correctly")
	}
}

func TestPrintJSON(t *testing.T) {
	tests := []struct {
		name     string
		data     any
		expected string
	}{
		{
			name: "Simple object",
			data: map[string]string{"key": "value"},
			expected: `{
  "key": "value"
}
`,
		},
		{
			name: "Array",
			data: []string{"item1", "item2", "item3"},
			expected: `[
  "item1",
  "item2",
  "item3"
]
`,
		},
		{
			name: "Nested object",
			data: map[string]any{
				"name": "test",
				"nested": map[string]int{
					"count": 42,
				},
			},
			expected: `{
  "name": "test",
  "nested": {
    "count": 42
  }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			p := New(OutputTypeTable, false)
			p.SetOutput(buf)

			err := p.PrintJSON(tt.data)
			if err != nil {
				t.Fatalf("PrintJSON() failed: %v", err)
			}

			if buf.String() != tt.expected {
				t.Errorf("Expected:\n%s\nGot:\n%s", tt.expected, buf.String())
			}
		})
	}
}

func TestPrintJSON_InvalidData(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(OutputTypeTable, false)
	p.SetOutput(buf)

	// Channels cannot be marshaled to JSON
	invalidData := make(chan int)

	err := p.PrintJSON(invalidData)
	if err == nil {
		t.Error("PrintJSON() should fail with invalid data")
	}
}

func TestPrintSuccess(t *testing.T) {
	// PrintSuccess writes to stdout
	// We just verify it doesn't panic
	PrintSuccess("Operation completed")
	// If we get here without panic, the test passes
}

func TestPrintError(t *testing.T) {
	// Since PrintError writes to stderr, we test that it doesn't panic
	PrintError("An error occurred")
	// If we get here without panic, the test passes
}

func TestPrintWarning(t *testing.T) {
	// PrintWarning writes to stdout
	// We just verify it doesn't panic
	PrintWarning("This is a warning")
	// If we get here without panic, the test passes
}

func TestPrintInfo(t *testing.T) {
	// PrintInfo writes to stdout
	// We just verify it doesn't panic
	PrintInfo("This is information")
	// If we get here without panic, the test passes
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "Specific time",
			time:     time.Date(2023, 10, 15, 14, 30, 45, 0, time.UTC),
			expected: "2023-10-15T14:30:45Z",
		},
		{
			name:     "Zero time",
			time:     time.Time{},
			expected: "0001-01-01T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatTimestamp(tt.time)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFormatTimestampShort(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "Specific time",
			time:     time.Date(2023, 10, 15, 14, 30, 45, 0, time.UTC),
			expected: "2023-10-15 14:30",
		},
		{
			name:     "Zero time",
			time:     time.Time{},
			expected: "0001-01-01 00:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatTimestampShort(tt.time)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "5 days ago",
			time:     now.Add(-5 * 24 * time.Hour),
			expected: "5d",
		},
		{
			name:     "1 day ago",
			time:     now.Add(-24 * time.Hour),
			expected: "1d",
		},
		{
			name:     "3 hours ago",
			time:     now.Add(-3 * time.Hour),
			expected: "3h",
		},
		{
			name:     "45 minutes ago",
			time:     now.Add(-45 * time.Minute),
			expected: "45m",
		},
		{
			name:     "30 seconds ago",
			time:     now.Add(-30 * time.Second),
			expected: "30s",
		},
		{
			name:     "Just now",
			time:     now,
			expected: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatAge(tt.time)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFormatAge_EdgeCases(t *testing.T) {
	now := time.Now()

	// 23 hours and 59 minutes should still be 23h, not 1d
	result := FormatAge(now.Add(-23*time.Hour - 59*time.Minute))
	if result != "23h" {
		t.Errorf("Expected 23h, got %s", result)
	}

	// 1 hour and 30 minutes should be 1h, not 90m
	result = FormatAge(now.Add(-1*time.Hour - 30*time.Minute))
	if result != "1h" {
		t.Errorf("Expected 1h, got %s", result)
	}

	// 59 seconds should be 59s, not 0m
	result = FormatAge(now.Add(-59 * time.Second))
	if result != "59s" {
		t.Errorf("Expected 59s, got %s", result)
	}
}

func TestFormatAge_FutureTime(t *testing.T) {
	// Test with future time (should handle negative duration gracefully)
	futureTime := time.Now().Add(5 * time.Hour)
	result := FormatAge(futureTime)

	// The function will return negative values for future times
	// This is expected behavior - we just verify it doesn't panic
	if result == "" {
		t.Error("FormatAge() should return a value for future times")
	}
}

func TestPrintJSON_ComplexStruct(t *testing.T) {
	type Server struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Active  bool   `json:"active"`
	}

	buf := &bytes.Buffer{}
	p := New(OutputTypeJSON, false)
	p.SetOutput(buf)

	server := Server{
		Name:    "test-server",
		Version: "1.0.0",
		Active:  true,
	}

	err := p.PrintJSON(server)
	if err != nil {
		t.Fatalf("PrintJSON() failed: %v", err)
	}

	// Verify it's valid JSON
	var result Server
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}

	if result.Name != server.Name {
		t.Errorf("Expected name %s, got %s", server.Name, result.Name)
	}
}
