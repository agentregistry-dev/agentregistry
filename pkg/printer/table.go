package printer

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// TablePrinter handles formatted table output similar to kubectl
type TablePrinter struct {
	writer     *tabwriter.Writer
	headers    []string
	rows       [][]string
	noHeaders  bool
	wide       bool
	outputType OutputType
}

// OutputType defines the output format
type OutputType string

const (
	// OutputTypeTable outputs in table format (default)
	OutputTypeTable OutputType = "table"
	// OutputTypeWide outputs in table format with additional columns
	OutputTypeWide OutputType = "wide"
	// OutputTypeJSON outputs in JSON format
	OutputTypeJSON OutputType = "json"
	// OutputTypeYAML outputs in YAML format
	OutputTypeYAML OutputType = "yaml"
)

// Option configures the TablePrinter
type Option func(*TablePrinter)

// WithNoHeaders disables header output
func WithNoHeaders() Option {
	return func(p *TablePrinter) {
		p.noHeaders = true
	}
}

// WithWide enables wide output format
func WithWide() Option {
	return func(p *TablePrinter) {
		p.wide = true
	}
}

// WithOutputType sets the output type
func WithOutputType(t OutputType) Option {
	return func(p *TablePrinter) {
		p.outputType = t
	}
}

// NewTablePrinter creates a new table printer with kubectl-style formatting
// It uses tabwriter for clean column alignment with minimal styling
func NewTablePrinter(out io.Writer, opts ...Option) *TablePrinter {
	if out == nil {
		out = os.Stdout
	}

	p := &TablePrinter{
		writer:     tabwriter.NewWriter(out, 0, 0, 3, ' ', 0),
		rows:       make([][]string, 0),
		outputType: OutputTypeTable,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// SetHeaders sets the table headers
func (p *TablePrinter) SetHeaders(headers ...string) {
	p.headers = headers
}

// AddRow adds a data row to the table
func (p *TablePrinter) AddRow(values ...any) {
	row := make([]string, len(values))
	for i, v := range values {
		row[i] = fmt.Sprintf("%v", v)
	}
	p.rows = append(p.rows, row)
}

// Render outputs the formatted table
func (p *TablePrinter) Render() error {
	if len(p.rows) == 0 && len(p.headers) == 0 {
		return nil
	}

	// Print headers
	if !p.noHeaders && len(p.headers) > 0 {
		headerLine := strings.ToUpper(strings.Join(p.headers, "\t"))
		_, _ = fmt.Fprintln(p.writer, headerLine)
	}

	// Print rows
	for _, row := range p.rows {
		_, _ = fmt.Fprintln(p.writer, strings.Join(row, "\t"))
	}

	return p.writer.Flush()
}

// PrintTable is a convenience function for simple table printing
func PrintTable(headers []string, rows [][]string, opts ...Option) error {
	printer := NewTablePrinter(os.Stdout, opts...)
	printer.SetHeaders(headers...)
	for _, row := range rows {
		values := make([]any, len(row))
		for i, v := range row {
			values[i] = v
		}
		printer.AddRow(values...)
	}
	return printer.Render()
}

// TruncateString truncates a string to maxLen with ellipsis
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// FormatStatus returns a status string with kubectl-style formatting
func FormatStatus(installed bool) string {
	if installed {
		return "Installed"
	}
	return "Available"
}

// EmptyValueOrDefault returns the value or a default placeholder
func EmptyValueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
