package printer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Printer handles various output formats
type Printer struct {
	out        io.Writer
	outputType OutputType
	wide       bool
}

// New creates a new printer with the specified output type
func New(outputType OutputType, wide bool) *Printer {
	return &Printer{
		out:        os.Stdout,
		outputType: outputType,
		wide:       wide,
	}
}

// SetOutput sets the output writer
func (p *Printer) SetOutput(out io.Writer) {
	p.out = out
}

// PrintJSON prints data in JSON format
func (p *Printer) PrintJSON(data any) error {
	encoder := json.NewEncoder(p.out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintSuccess prints a success message with kubectl-style formatting
func PrintSuccess(message string) {
	_, _ = fmt.Fprintf(os.Stdout, "✓ %s\n", message)
}

// PrintError prints an error message
func PrintError(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
}

// PrintWarning prints a warning message
func PrintWarning(message string) {
	_, _ = fmt.Fprintf(os.Stdout, "Warning: %s\n", message)
}

// PrintInfo prints an info message
func PrintInfo(message string) {
	_, _ = fmt.Fprintf(os.Stdout, "%s\n", message)
}

// FormatTimestamp formats a timestamp in kubectl style
func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02T15:04:05Z")
}

// FormatTimestampShort formats a timestamp in short format
func FormatTimestampShort(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// FormatAge formats time.Duration as a kubectl-style age string (e.g., "5d", "3h", "45m")
func FormatAge(t time.Time) string {
	duration := time.Since(t)

	days := int(duration.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}

	hours := int(duration.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}

	minutes := int(duration.Minutes())
	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}

	seconds := int(duration.Seconds())
	return fmt.Sprintf("%ds", seconds)
}
