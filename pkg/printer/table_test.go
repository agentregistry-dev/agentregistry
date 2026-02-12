package printer

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewTablePrinter(t *testing.T) {
	buf := &bytes.Buffer{}

	tests := []struct {
		name string
		opts []Option
	}{
		{"No options", []Option{}},
		{"With no headers", []Option{WithNoHeaders()}},
		{"With wide", []Option{WithWide()}},
		{"With JSON output", []Option{WithOutputType(OutputTypeJSON)}},
		{"Multiple options", []Option{WithNoHeaders(), WithWide()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewTablePrinter(buf, tt.opts...)

			if p == nil {
				t.Fatal("NewTablePrinter() returned nil")
			}

			if p.writer == nil {
				t.Error("TablePrinter writer is nil")
			}

			if p.rows == nil {
				t.Error("TablePrinter rows is nil")
			}
		})
	}
}

func TestNewTablePrinter_NilWriter(t *testing.T) {
	// Should default to os.Stdout
	p := NewTablePrinter(nil)

	if p == nil {
		t.Fatal("NewTablePrinter() with nil writer returned nil")
	}

	if p.writer == nil {
		t.Error("TablePrinter writer should not be nil even with nil input")
	}
}

func TestSetHeaders(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	headers := []string{"Name", "Version", "Status"}
	p.SetHeaders(headers...)

	if len(p.headers) != len(headers) {
		t.Errorf("Expected %d headers, got %d", len(headers), len(p.headers))
	}

	for i, h := range headers {
		if p.headers[i] != h {
			t.Errorf("Header %d: expected %s, got %s", i, h, p.headers[i])
		}
	}
}

func TestAddRow(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	tests := []struct {
		name   string
		values []any
	}{
		{"String values", []any{"server1", "1.0.0", "active"}},
		{"Mixed types", []any{"server2", 123, true}},
		{"With nil", []any{"server3", nil, "active"}},
		{"Empty row", []any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialCount := len(p.rows)
			p.AddRow(tt.values...)

			if len(p.rows) != initialCount+1 {
				t.Errorf("Expected %d rows, got %d", initialCount+1, len(p.rows))
			}

			lastRow := p.rows[len(p.rows)-1]
			if len(lastRow) != len(tt.values) {
				t.Errorf("Expected row length %d, got %d", len(tt.values), len(lastRow))
			}
		})
	}
}

func TestRender_BasicTable(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	p.SetHeaders("Name", "Version", "Status")
	p.AddRow("server1", "1.0.0", "active")
	p.AddRow("server2", "2.0.0", "inactive")

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed: %v", err)
	}

	output := buf.String()

	// Check headers are uppercase
	if !strings.Contains(output, "NAME") {
		t.Error("Headers should be uppercase")
	}

	// Check rows are present
	if !strings.Contains(output, "server1") {
		t.Error("Output should contain 'server1'")
	}
	if !strings.Contains(output, "server2") {
		t.Error("Output should contain 'server2'")
	}

	// Check values
	if !strings.Contains(output, "1.0.0") {
		t.Error("Output should contain '1.0.0'")
	}
	if !strings.Contains(output, "active") {
		t.Error("Output should contain 'active'")
	}
}

func TestRender_NoHeaders(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf, WithNoHeaders())

	p.SetHeaders("Name", "Version")
	p.AddRow("server1", "1.0.0")

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed: %v", err)
	}

	output := buf.String()

	// Headers should not appear
	if strings.Contains(output, "NAME") {
		t.Error("Headers should not be present with WithNoHeaders()")
	}

	// But data should be there
	if !strings.Contains(output, "server1") {
		t.Error("Output should contain 'server1'")
	}
}

func TestRender_EmptyTable(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed on empty table: %v", err)
	}

	if buf.Len() != 0 {
		t.Error("Empty table should produce no output")
	}
}

func TestRender_OnlyHeaders(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	p.SetHeaders("Name", "Version")

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed: %v", err)
	}

	output := buf.String()

	// Should contain headers even with no rows
	if !strings.Contains(output, "NAME") {
		t.Error("Should output headers even with no rows")
	}
}

func TestPrintTable(t *testing.T) {
	buf := &bytes.Buffer{}

	headers := []string{"Name", "Age", "City"}
	rows := [][]string{
		{"Alice", "30", "New York"},
		{"Bob", "25", "Los Angeles"},
		{"Charlie", "35", "Chicago"},
	}

	// We can't easily test PrintTable since it writes to os.Stdout
	// But we can test that it doesn't panic
	// For actual output testing, we'd need to mock os.Stdout

	// Create our own printer to test the logic
	p := NewTablePrinter(buf)
	p.SetHeaders(headers...)
	for _, row := range rows {
		values := make([]any, len(row))
		for i, v := range row {
			values[i] = v
		}
		p.AddRow(values...)
	}

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed: %v", err)
	}

	output := buf.String()

	// Verify all content is present
	for _, header := range headers {
		if !strings.Contains(output, strings.ToUpper(header)) {
			t.Errorf("Output should contain header %s", header)
		}
	}

	for _, row := range rows {
		for _, cell := range row {
			if !strings.Contains(output, cell) {
				t.Errorf("Output should contain %s", cell)
			}
		}
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "String shorter than max",
			input:    "short",
			maxLen:   10,
			expected: "short",
		},
		{
			name:     "String equal to max",
			input:    "exactlyten",
			maxLen:   10,
			expected: "exactlyten",
		},
		{
			name:     "String longer than max",
			input:    "this is a very long string",
			maxLen:   10,
			expected: "this is...",
		},
		{
			name:     "Max length 3",
			input:    "hello",
			maxLen:   3,
			expected: "hel",
		},
		{
			name:     "Max length 1",
			input:    "hello",
			maxLen:   1,
			expected: "h",
		},
		{
			name:     "Empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "Unicode string",
			input:    "Hello World 世界",
			maxLen:   15,
			expected: "Hello World ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}

			// Verify result is not longer than maxLen
			if len(result) > tt.maxLen {
				t.Errorf("Result length %d exceeds maxLen %d", len(result), tt.maxLen)
			}
		})
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		name      string
		installed bool
		expected  string
	}{
		{"Installed", true, "Installed"},
		{"Not installed", false, "Available"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatStatus(tt.installed)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestEmptyValueOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		defaultValue string
		expected     string
	}{
		{
			name:         "Non-empty value",
			value:        "actual",
			defaultValue: "default",
			expected:     "actual",
		},
		{
			name:         "Empty value",
			value:        "",
			defaultValue: "default",
			expected:     "default",
		},
		{
			name:         "Both empty",
			value:        "",
			defaultValue: "",
			expected:     "",
		},
		{
			name:         "Whitespace value",
			value:        "   ",
			defaultValue: "default",
			expected:     "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EmptyValueOrDefault(tt.value, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestWithNoHeaders(t *testing.T) {
	buf := &bytes.Buffer{}
	opt := WithNoHeaders()
	p := NewTablePrinter(buf)

	opt(p)

	if !p.noHeaders {
		t.Error("WithNoHeaders() should set noHeaders to true")
	}
}

func TestWithWide(t *testing.T) {
	buf := &bytes.Buffer{}
	opt := WithWide()
	p := NewTablePrinter(buf)

	opt(p)

	if !p.wide {
		t.Error("WithWide() should set wide to true")
	}
}

func TestWithOutputType(t *testing.T) {
	tests := []struct {
		name       string
		outputType OutputType
	}{
		{"Table", OutputTypeTable},
		{"Wide", OutputTypeWide},
		{"JSON", OutputTypeJSON},
		{"YAML", OutputTypeYAML},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			opt := WithOutputType(tt.outputType)
			p := NewTablePrinter(buf)

			opt(p)

			if p.outputType != tt.outputType {
				t.Errorf("Expected outputType %s, got %s", tt.outputType, p.outputType)
			}
		})
	}
}

func TestRender_MultipleRows(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	p.SetHeaders("Col1", "Col2", "Col3")

	// Add many rows
	for i := range 100 {
		p.AddRow(i, i*2, i*3)
	}

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed with many rows: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have 101 lines (1 header + 100 data rows)
	if len(lines) != 101 {
		t.Errorf("Expected 101 lines, got %d", len(lines))
	}
}

func TestRender_SpecialCharacters(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	p.SetHeaders("Name", "Description")
	p.AddRow("test", "Special chars: !@#$%^&*()")
	p.AddRow("unicode", "Unicode: 你好世界 🎉")
	p.AddRow("tabs", "With\ttabs")

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed with special characters: %v", err)
	}

	output := buf.String()

	// Verify special characters are preserved
	if !strings.Contains(output, "!@#$%^&*()") {
		t.Error("Special characters should be preserved")
	}
	if !strings.Contains(output, "你好世界") {
		t.Error("Unicode characters should be preserved")
	}
}

func TestAddRow_TypeConversion(t *testing.T) {
	buf := &bytes.Buffer{}
	p := NewTablePrinter(buf)

	p.SetHeaders("String", "Int", "Float", "Bool", "Nil")
	p.AddRow("text", 42, 3.14, true, nil)

	err := p.Render()
	if err != nil {
		t.Fatalf("Render() failed: %v", err)
	}

	output := buf.String()

	// Verify type conversions
	if !strings.Contains(output, "text") {
		t.Error("String should be preserved")
	}
	if !strings.Contains(output, "42") {
		t.Error("Int should be converted to string")
	}
	if !strings.Contains(output, "3.14") {
		t.Error("Float should be converted to string")
	}
	if !strings.Contains(output, "true") {
		t.Error("Bool should be converted to string")
	}
	if !strings.Contains(output, "<nil>") {
		t.Error("Nil should be converted to '<nil>'")
	}
}
