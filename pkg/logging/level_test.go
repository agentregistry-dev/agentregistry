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
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestLogging(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		components     []string
		query          string
		setLevel       map[string]slog.Level
		wantStatusCode int
		wantBody       string
		wantLevels     map[string]slog.Level
	}{
		{
			name:           "GET returns current levels",
			method:         http.MethodGet,
			wantStatusCode: http.StatusOK,
			wantBody:       "current log levels:",
			wantLevels: map[string]slog.Level{
				DefaultComponent: globalLevel.Level(),
			},
		},
		{
			name:           "POST with no params returns 400",
			method:         http.MethodPost,
			wantStatusCode: http.StatusBadRequest,
			wantBody:       "query parameters required",
			wantLevels: map[string]slog.Level{
				DefaultComponent: slog.LevelInfo,
			},
		},
		{
			name:           "update default level to debug",
			query:          "level=debug",
			wantStatusCode: http.StatusOK,
			wantLevels: map[string]slog.Level{
				DefaultComponent: slog.LevelDebug,
			},
		},
		{
			name:           "update all loggers to debug level",
			components:     []string{"c1", "c2", "c3"},
			query:          "level=debug",
			wantStatusCode: http.StatusOK,
			wantLevels: map[string]slog.Level{
				DefaultComponent: slog.LevelDebug,
				"c1":             slog.LevelDebug,
				"c2":             slog.LevelDebug,
				"c3":             slog.LevelDebug,
			},
		},
		{
			name:           "ignore component levels when updating specific logger levels",
			components:     []string{"c1", "c2", "c3"},
			query:          "level=debug&c1=error&c2=warn&c3=trace",
			wantStatusCode: http.StatusOK,
			wantLevels: map[string]slog.Level{
				DefaultComponent: slog.LevelDebug,
				"c1":             slog.LevelDebug,
				"c2":             slog.LevelDebug,
				"c3":             slog.LevelDebug,
			},
		},
		{
			name:           "update default and component levels",
			components:     []string{"c1", "c2", "c3"},
			query:          "default=debug&c1=error&c2=warn&c3=trace",
			wantStatusCode: http.StatusOK,
			wantLevels: map[string]slog.Level{
				DefaultComponent: slog.LevelDebug,
				"c1":             slog.LevelError,
				"c2":             slog.LevelWarn,
				"c3":             LevelTrace,
			},
		},
		{
			name:           "incorrect global log level should error and preserve current level",
			query:          "level=foo",
			wantStatusCode: http.StatusBadRequest,
			wantBody:       "unknown log level foo",
			wantLevels: map[string]slog.Level{
				DefaultComponent: slog.LevelInfo,
			},
		},
		{
			name:           "incorrect component log level should error and preserve current level",
			components:     []string{"c1"},
			query:          "c1=foo",
			wantStatusCode: http.StatusBadRequest,
			wantBody:       "component c1: unknown log level foo",
			wantLevels: map[string]slog.Level{
				"c1": slog.LevelInfo,
			},
		},
		{
			name:           "unknown component returns 400 without partial update",
			components:     []string{"c1"},
			query:          "c1=debug&unknown=error",
			wantStatusCode: http.StatusBadRequest,
			wantBody:       "unknown component: unknown",
			wantLevels: map[string]slog.Level{
				"c1": slog.LevelInfo, // must not have been updated
			},
		},
		{
			name:           "update default and component levels using SetLevel",
			method:         http.MethodGet,
			components:     []string{"c1", "c2", "c3"},
			setLevel:       map[string]slog.Level{"default": slog.LevelDebug, "c1": slog.LevelError, "c2": slog.LevelWarn, "c3": LevelTrace},
			wantStatusCode: http.StatusOK,
			wantLevels: map[string]slog.Level{
				DefaultComponent: slog.LevelDebug,
				"c1":             slog.LevelError,
				"c2":             slog.LevelWarn,
				"c3":             LevelTrace,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := require.New(t)

			// Reset component levels to default level
			Reset(slog.LevelInfo)

			loggers := map[string]*slog.Logger{DefaultComponent: slog.Default()}
			for _, component := range tc.components {
				logger := New(component)
				t.Cleanup(func() { DeleteLeveler(component) }) //nolint: errcheck
				a.NotNil(logger)
				loggers[component] = logger
			}

			// Test HTTP handler
			method := tc.method
			if method == "" {
				method = http.MethodPost
			}
			path := "/logging"
			if tc.query != "" {
				path += "?" + tc.query
			}
			req := httptest.NewRequest(method, path, nil)
			w := httptest.NewRecorder()
			HTTPLevelHandler(w, req)
			resp := w.Result()
			a.Equal(tc.wantStatusCode, resp.StatusCode)
			data, err := io.ReadAll(resp.Body)
			a.NoError(err)
			a.NotEmpty(data)
			a.Contains(string(data), tc.wantBody)

			// Test SetLevel
			for component, level := range tc.setLevel {
				err := SetLevel(component, level)
				a.NoError(err)
			}

			for component, level := range tc.wantLevels {
				a.Equal(level, MustGetLevel(component), component)
				a.True(loggers[component].Enabled(context.TODO(), level), component)
			}
		})
	}
}

func TestGetComponentLevels(t *testing.T) {
	a := assert.New(t)

	_ = NewWithOptions("TestGetComponentLevels1", Options{Level: ptr.To(slog.LevelDebug)})
	t.Cleanup(func() { DeleteLeveler("TestGetComponentLevels1") }) //nolint: errcheck
	_ = NewWithOptions("TestGetComponentLevels2", Options{Level: ptr.To(slog.LevelError)})
	t.Cleanup(func() { DeleteLeveler("TestGetComponentLevels2") }) //nolint: errcheck

	got := GetComponentLevels()
	a.Equal(slog.LevelDebug, got["TestGetComponentLevels1"], "TestGetComponentLevels1")
	a.Equal(slog.LevelError, got["TestGetComponentLevels2"], "TestGetComponentLevels2")
}

func TestLocalhostOnly(t *testing.T) {
	handler := LocalhostOnly(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint: errcheck
	})

	tests := []struct {
		name       string
		remoteAddr string
		wantStatus int
	}{
		{
			name:       "IPv4 localhost allowed",
			remoteAddr: "127.0.0.1:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "IPv6 localhost allowed",
			remoteAddr: "[::1]:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "remote address rejected",
			remoteAddr: "192.168.1.100:12345",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "public IP rejected",
			remoteAddr: "8.8.8.8:443",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/logging", nil)
			req.RemoteAddr = tc.remoteAddr
			w := httptest.NewRecorder()
			handler(w, req)
			assert.Equal(t, tc.wantStatus, w.Result().StatusCode)
		})
	}
}
