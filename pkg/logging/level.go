/*
Portions of this file are derived from the slog-leveler project
(https://github.com/shashankram/slog-leveler)
which is licensed under the MIT License.

# MIT License

# Copyright (c) 2025 Shashank Ram

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
package logging

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Extra slog log levels
const (
	LevelTrace = slog.Level(-5) // 1 lower than slog.LevelDebug
)

const (
	levelQuery = "level"
)

// Level strings
const (
	errorLevel = "error"
	warnLevel  = "warn"
	infoLevel  = "info"
	debugLevel = "debug"
	traceLevel = "trace"
)

var (
	// globalLevel is the slog.LevelVar for the default logger
	globalLevel = &slog.LevelVar{} // default is INFO
)

// GetLevel returns the current log level for the component
func GetLevel(component string) (slog.Level, error) {
	if component == "" {
		component = DefaultComponent
	}
	lvl, ok := componentLeveler.Load(component)
	if !ok {
		return slog.Level(0), fmt.Errorf("logger not found for component: %s", component)
	}
	levelr := lvl.(*slog.LevelVar)
	return levelr.Level(), nil
}

// MustGetLevel returns the current log level for the component or panics if the component is not found
func MustGetLevel(component string) slog.Level {
	level, err := GetLevel(component)
	if err != nil {
		panic(err)
	}
	return level
}

// SetLevel sets the log level for the component
func SetLevel(component string, level slog.Level) error {
	if component == "" {
		component = DefaultComponent
	}
	lvl, ok := componentLeveler.Load(component)
	if !ok {
		return fmt.Errorf("logger not found for component: %s", component)
	}
	levelr := lvl.(*slog.LevelVar)
	levelr.Set(level)
	return nil
}

// MustSetLevel sets the log level for the component or panics if the component is not found
func MustSetLevel(component string, level slog.Level) {
	if err := SetLevel(component, level); err != nil {
		panic(err)
	}
}

// Reset resets the log level for all components to the given level
func Reset(level slog.Level) {
	componentLeveler.Range(func(key any, value any) bool {
		MustSetLevel(key.(string), level)
		return true
	})
}

// HTTPLevelHandler handles HTTP requests to the log level of the default or
// component specific loggers.
//
// GET returns the current log levels of all components.
//
// POST/PUT with query parameters updates log levels:
//   - level=<level>: updates log level across all component loggers
//   - <component>=<level>&<component>=<level2>...: updates log level for specific components
//
// POST/PUT without query parameters returns 400.
func HTTPLevelHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeCurrentLevels(w)
		return
	case http.MethodPost, http.MethodPut:
		// handled below
	default:
		http.Error(w, "method must be one of GET|POST|PUT", http.StatusMethodNotAllowed)
		return
	}

	componentValues := r.URL.Query()
	if len(componentValues) == 0 {
		http.Error(w, "query parameters required", http.StatusBadRequest)
		return
	}

	if lvl := componentValues.Get(levelQuery); lvl != "" {
		level, err := ParseLevel(lvl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		Reset(level)
		w.Write(fmt.Appendf(nil, "all logger levels updated to level: %s\n", lvl)) //nolint: errcheck
		return
	}

	levels := make(map[string]slog.Level)
	// Parse ?c1=level1&c2=level2,...
	for component := range componentValues {
		l := componentValues.Get(component)
		if l == "" {
			http.Error(w, fmt.Sprintf("component %s: empty value", component), http.StatusBadRequest)
			return
		}

		level, err := ParseLevel(l)
		if err != nil {
			http.Error(w, fmt.Sprintf("component %s: %v", component, err), http.StatusBadRequest)
			return
		}
		levels[component] = level
	}

	// Validate all components exist before applying any changes
	for component := range levels {
		if _, ok := componentLeveler.Load(component); !ok {
			http.Error(w, fmt.Sprintf("unknown component: %s", component), http.StatusBadRequest)
			return
		}
	}

	// Apply all changes (guaranteed to succeed)
	for component, level := range levels {
		MustSetLevel(component, level)
		w.Write(fmt.Appendf(nil, "component %s log level set to: %s\n", component, LevelToString(level))) //nolint: errcheck
	}
}

// writeCurrentLevels writes the current log levels of all components to the response
func writeCurrentLevels(w http.ResponseWriter) {
	w.Write([]byte("current log levels:\n---\n")) //nolint: errcheck
	componentLeveler.Range(func(key any, value any) bool {
		w.Write(fmt.Appendf(nil, "%s: %s\n", key, LevelToString(value.(*slog.LevelVar).Level()))) //nolint: errcheck
		return true
	})
}

// LocalhostOnly wraps an http.HandlerFunc to only allow requests from localhost
func LocalhostOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden: localhost access only", http.StatusForbidden)
			return
		}
		if host != "127.0.0.1" && host != "::1" {
			http.Error(w, "forbidden: localhost access only", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// slogLevelReplacer replaces the slog.Level with a string representation
func slogLevelReplacer(groups []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.LevelKey {
		level := attr.Value.Any().(slog.Level)
		attr.Value = slog.StringValue(LevelToString(level))
	}
	return attr
}

// ParseLevel parses the given level string to slog.Level,
// and returns an error if the level is unknown
func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case traceLevel:
		return LevelTrace, nil
	case debugLevel:
		return slog.LevelDebug, nil
	case infoLevel:
		return slog.LevelInfo, nil
	case warnLevel:
		return slog.LevelWarn, nil
	case errorLevel:
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %s; should be one of error|warn|info|debug|trace", level)
	}
}

// LevelToString returns the string representation of slog.Level
func LevelToString(level slog.Level) string {
	switch level {
	case LevelTrace:
		return traceLevel
	case slog.LevelDebug:
		return debugLevel
	case slog.LevelInfo:
		return infoLevel
	case slog.LevelWarn:
		return warnLevel
	case slog.LevelError:
		return errorLevel
	default:
		return level.String()
	}
}
