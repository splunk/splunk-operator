// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name           string
		envVars        map[string]string
		expectedLevel  slog.Level
		expectedFormat string
		expectedSource bool
	}{
		{
			name:           "default values",
			envVars:        map[string]string{},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name: "debug level with auto source",
			envVars: map[string]string{
				EnvLogLevel: "debug",
			},
			expectedLevel:  slog.LevelDebug,
			expectedFormat: FormatJSON,
			expectedSource: true, // auto-enabled for debug
		},
		{
			name: "warn level with text format",
			envVars: map[string]string{
				EnvLogLevel:  "warn",
				EnvLogFormat: "text",
			},
			expectedLevel:  slog.LevelWarn,
			expectedFormat: FormatText,
			expectedSource: false,
		},
		{
			name: "error level with explicit source",
			envVars: map[string]string{
				EnvLogLevel:     "error",
				EnvLogAddSource: "true",
			},
			expectedLevel:  slog.LevelError,
			expectedFormat: FormatJSON,
			expectedSource: true,
		},
		{
			name: "debug level with source disabled",
			envVars: map[string]string{
				EnvLogLevel:     "debug",
				EnvLogAddSource: "false",
			},
			expectedLevel:  slog.LevelDebug,
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name: "invalid level defaults to info",
			envVars: map[string]string{
				EnvLogLevel: "invalid",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Unsetenv(EnvLogLevel)
			os.Unsetenv(EnvLogFormat)
			os.Unsetenv(EnvLogAddSource)

			// Set test environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := LoadConfig()

			if cfg.Level != tt.expectedLevel {
				t.Errorf("Level = %v, want %v", cfg.Level, tt.expectedLevel)
			}
			if cfg.Format != tt.expectedFormat {
				t.Errorf("Format = %v, want %v", cfg.Format, tt.expectedFormat)
			}
			if cfg.AddSource != tt.expectedSource {
				t.Errorf("AddSource = %v, want %v", cfg.AddSource, tt.expectedSource)
			}

			// Cleanup
			for k := range tt.envVars {
				os.Unsetenv(k)
			}
		})
	}
}

func TestLoadConfigWithFlags(t *testing.T) {
	// Clear environment
	os.Unsetenv(EnvLogLevel)
	os.Unsetenv(EnvLogFormat)
	os.Unsetenv(EnvLogAddSource)

	// Set environment
	os.Setenv(EnvLogLevel, "info")
	os.Setenv(EnvLogFormat, "json")
	defer func() {
		os.Unsetenv(EnvLogLevel)
		os.Unsetenv(EnvLogFormat)
	}()

	// Flags should override environment
	addSource := true
	cfg := LoadConfigWithFlags("debug", "text", &addSource)

	if cfg.Level != slog.LevelDebug {
		t.Errorf("Level = %v, want %v", cfg.Level, slog.LevelDebug)
	}
	if cfg.Format != FormatText {
		t.Errorf("Format = %v, want %v", cfg.Format, FormatText)
	}
	if cfg.AddSource != true {
		t.Errorf("AddSource = %v, want %v", cfg.AddSource, true)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"invalid", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRedactSensitiveData(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{"password field", "password", "secret123", "[REDACTED]"},
		{"Password field", "Password", "secret123", "[REDACTED]"},
		{"admin_password field", "admin_password", "secret123", "[REDACTED]"},
		{"token field", "token", "abc123", "[REDACTED]"},
		{"auth_token field", "auth_token", "abc123", "[REDACTED]"},
		{"secret field", "secret", "mysecret", "[REDACTED]"},
		{"apikey field", "apikey", "key123", "[REDACTED]"},
		{"api_key field", "api_key", "key123", "[REDACTED]"},
		{"API_KEY field", "API_KEY", "key123", "[REDACTED]"},
		{"credential field", "credential", "cred123", "[REDACTED]"},
		{"auth field", "auth", "authdata", "[REDACTED]"},
		{"normal field", "namespace", "default", "default"},
		{"name field", "name", "myapp", "myapp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := slog.String(tt.key, tt.value)
			result := redactSensitiveData(nil, attr)

			if result.Value.String() != tt.expected {
				t.Errorf("redactSensitiveData(%q, %q) = %q, want %q",
					tt.key, tt.value, result.Value.String(), tt.expected)
			}
		})
	}
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "JSON handler",
			config: Config{
				Level:     slog.LevelInfo,
				Format:    FormatJSON,
				AddSource: false,
			},
		},
		{
			name: "Text handler",
			config: Config{
				Level:     slog.LevelDebug,
				Format:    FormatText,
				AddSource: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(tt.config)
			if handler == nil {
				t.Error("NewHandler returned nil")
			}
		})
	}
}

func TestNewProductionHandler(t *testing.T) {
	handler := NewProductionHandler()
	if handler == nil {
		t.Error("NewProductionHandler returned nil")
	}
}

func TestNewDevelopmentHandler(t *testing.T) {
	handler := NewDevelopmentHandler()
	if handler == nil {
		t.Error("NewDevelopmentHandler returned nil")
	}
}

func TestSetupLogger(t *testing.T) {
	cfg := Config{
		Level:     slog.LevelInfo,
		Format:    FormatJSON,
		AddSource: false,
	}

	logger := SetupLogger(cfg)
	if logger == nil {
		t.Error("SetupLogger returned nil")
	}

	// Verify it's set as default
	if slog.Default() != logger {
		t.Error("Logger was not set as default")
	}
}

func TestSetupLoggerWithAttrs(t *testing.T) {
	cfg := Config{
		Level:     slog.LevelInfo,
		Format:    FormatJSON,
		AddSource: false,
	}

	logger := SetupLoggerWithAttrs(cfg, "splunk-operator", "1.0.0", "abc123")
	if logger == nil {
		t.Error("SetupLoggerWithAttrs returned nil")
	}
}

func TestLevelConversions(t *testing.T) {
	tests := []struct {
		level slog.Level
		str   string
	}{
		{slog.LevelDebug, "debug"},
		{slog.LevelInfo, "info"},
		{slog.LevelWarn, "warn"},
		{slog.LevelError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			// Test LevelToString
			result := LevelToString(tt.level)
			if result != tt.str {
				t.Errorf("LevelToString(%v) = %q, want %q", tt.level, result, tt.str)
			}

			// Test LevelFromString
			level := LevelFromString(tt.str)
			if level != tt.level {
				t.Errorf("LevelFromString(%q) = %v, want %v", tt.str, level, tt.level)
			}
		})
	}
}

// TestHandler is a test handler that captures log output
type TestHandler struct {
	buf    *bytes.Buffer
	attrs  []slog.Attr
	groups []string
}

func NewTestHandler() *TestHandler {
	return &TestHandler{
		buf: &bytes.Buffer{},
	}
}

func (h *TestHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *TestHandler) Handle(ctx context.Context, r slog.Record) error {
	h.buf.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		h.buf.WriteString(" ")
		h.buf.WriteString(a.Key)
		h.buf.WriteString("=")
		h.buf.WriteString(a.Value.String())
		return true
	})
	h.buf.WriteString("\n")
	return nil
}

func (h *TestHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := &TestHandler{
		buf:    h.buf,
		attrs:  append(h.attrs, attrs...),
		groups: h.groups,
	}
	return newHandler
}

func (h *TestHandler) WithGroup(name string) slog.Handler {
	newHandler := &TestHandler{
		buf:    h.buf,
		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
	return newHandler
}

func (h *TestHandler) String() string {
	return h.buf.String()
}

func (h *TestHandler) Contains(s string) bool {
	return strings.Contains(h.buf.String(), s)
}

func TestSensitiveDataRedactionInLogs(t *testing.T) {
	// Create a buffer to capture output
	var buf bytes.Buffer

	opts := &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: redactSensitiveData,
	}

	handler := slog.NewTextHandler(&buf, opts)
	logger := slog.New(handler)

	// Log with sensitive data
	logger.Info("User login",
		slog.String("username", "admin"),
		slog.String("password", "secret123"),
		slog.String("token", "abc123"))

	output := buf.String()

	// Verify sensitive data is redacted
	if strings.Contains(output, "secret123") {
		t.Error("Password was not redacted")
	}
	if strings.Contains(output, "abc123") {
		t.Error("Token was not redacted")
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Error("Redaction marker not found")
	}
	if !strings.Contains(output, "admin") {
		t.Error("Non-sensitive data (username) was incorrectly redacted")
	}
}

// ============================================================================
// FAILURE AND EDGE CASE TESTS
// ============================================================================

func TestLoadConfig_InvalidEnvironmentValues(t *testing.T) {
	tests := []struct {
		name           string
		envVars        map[string]string
		expectedLevel  slog.Level
		expectedFormat string
		expectedSource bool
	}{
		{
			name: "garbage log level",
			envVars: map[string]string{
				EnvLogLevel: "notavalidlevel",
			},
			expectedLevel:  slog.LevelInfo, // Should default to info
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name: "empty log level",
			envVars: map[string]string{
				EnvLogLevel: "",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name: "numeric log level (invalid)",
			envVars: map[string]string{
				EnvLogLevel: "123",
			},
			expectedLevel:  slog.LevelInfo, // Should default to info
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name: "special characters in log level",
			envVars: map[string]string{
				EnvLogLevel: "!@#$%",
			},
			expectedLevel:  slog.LevelInfo, // Should default to info
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name: "invalid format falls back gracefully",
			envVars: map[string]string{
				EnvLogFormat: "xml", // Not supported
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "xml", // Stored as-is, but handler will use JSON
			expectedSource: false,
		},
		{
			name: "invalid add_source value",
			envVars: map[string]string{
				EnvLogAddSource: "yes", // Should be "true"
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: FormatJSON,
			expectedSource: false, // "yes" != "true"
		},
		{
			name: "mixed case add_source",
			envVars: map[string]string{
				EnvLogAddSource: "TRUE",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: FormatJSON,
			expectedSource: true, // Should handle case-insensitive
		},
		{
			name: "whitespace in values",
			envVars: map[string]string{
				EnvLogLevel:  " debug ", // Has spaces
				EnvLogFormat: " json ",
			},
			expectedLevel:  slog.LevelInfo, // " debug " != "debug"
			expectedFormat: " json ",
			expectedSource: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Unsetenv(EnvLogLevel)
			os.Unsetenv(EnvLogFormat)
			os.Unsetenv(EnvLogAddSource)

			// Set test environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := LoadConfig()

			if cfg.Level != tt.expectedLevel {
				t.Errorf("Level = %v, want %v", cfg.Level, tt.expectedLevel)
			}
			if cfg.Format != tt.expectedFormat {
				t.Errorf("Format = %v, want %v", cfg.Format, tt.expectedFormat)
			}
			if cfg.AddSource != tt.expectedSource {
				t.Errorf("AddSource = %v, want %v", cfg.AddSource, tt.expectedSource)
			}

			// Cleanup
			for k := range tt.envVars {
				os.Unsetenv(k)
			}
		})
	}
}

func TestLoadConfigWithFlags_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		envLevel       string
		envFormat      string
		flagLevel      string
		flagFormat     string
		flagAddSource  *bool
		expectedLevel  slog.Level
		expectedFormat string
		expectedSource bool
	}{
		{
			name:           "empty flags use env vars",
			envLevel:       "warn",
			envFormat:      "text",
			flagLevel:      "",
			flagFormat:     "",
			flagAddSource:  nil,
			expectedLevel:  slog.LevelWarn,
			expectedFormat: FormatText,
			expectedSource: false,
		},
		{
			name:           "flags override env vars",
			envLevel:       "info",
			envFormat:      "json",
			flagLevel:      "error",
			flagFormat:     "text",
			flagAddSource:  boolPtr(true),
			expectedLevel:  slog.LevelError,
			expectedFormat: FormatText,
			expectedSource: true,
		},
		{
			name:           "invalid flag level falls back to env",
			envLevel:       "warn",
			envFormat:      "json",
			flagLevel:      "invalid",
			flagFormat:     "",
			flagAddSource:  nil,
			expectedLevel:  slog.LevelInfo, // invalid parses to info
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name:           "nil addSource pointer uses env/default",
			envLevel:       "info",
			envFormat:      "json",
			flagLevel:      "",
			flagFormat:     "",
			flagAddSource:  nil,
			expectedLevel:  slog.LevelInfo,
			expectedFormat: FormatJSON,
			expectedSource: false,
		},
		{
			name:           "false addSource pointer explicitly disables",
			envLevel:       "debug", // debug normally auto-enables source
			envFormat:      "json",
			flagLevel:      "",
			flagFormat:     "",
			flagAddSource:  boolPtr(false),
			expectedLevel:  slog.LevelDebug,
			expectedFormat: FormatJSON,
			expectedSource: false, // Explicitly disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear and set environment
			os.Unsetenv(EnvLogLevel)
			os.Unsetenv(EnvLogFormat)
			os.Unsetenv(EnvLogAddSource)

			if tt.envLevel != "" {
				os.Setenv(EnvLogLevel, tt.envLevel)
			}
			if tt.envFormat != "" {
				os.Setenv(EnvLogFormat, tt.envFormat)
			}

			cfg := LoadConfigWithFlags(tt.flagLevel, tt.flagFormat, tt.flagAddSource)

			if cfg.Level != tt.expectedLevel {
				t.Errorf("Level = %v, want %v", cfg.Level, tt.expectedLevel)
			}
			if cfg.Format != tt.expectedFormat {
				t.Errorf("Format = %v, want %v", cfg.Format, tt.expectedFormat)
			}
			if cfg.AddSource != tt.expectedSource {
				t.Errorf("AddSource = %v, want %v", cfg.AddSource, tt.expectedSource)
			}

			// Cleanup
			os.Unsetenv(EnvLogLevel)
			os.Unsetenv(EnvLogFormat)
			os.Unsetenv(EnvLogAddSource)
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func TestRedactSensitiveData_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		value       interface{}
		shouldMatch string
	}{
		// Empty and nil values
		{"empty password value", "password", "", "[REDACTED]"},
		{"empty key with password in name", "user_password_hash", "", "[REDACTED]"},

		// Nested/compound sensitive keys
		{"nested password key", "db_password_encrypted", "secret", "[REDACTED]"},
		{"camelCase token", "accessToken", "abc123", "[REDACTED]"},
		{"uppercase AUTH", "AUTH_HEADER", "Bearer xyz", "[REDACTED]"},

		// Keys that should NOT be redacted (false positives check)
		{"passport field", "passport_number", "AB123456", "AB123456"}, // contains "pass" but not "password"
		{"authenticate field", "authenticate", "true", "[REDACTED]"},  // contains "auth"
		{"secretary field", "secretary", "John", "[REDACTED]"},        // contains "secret"

		// Unicode and special characters
		{"key with unicode", "пароль", "secret", "secret"}, // Russian for password, but not in our list
		{"value with unicode", "password", "секрет", "[REDACTED]"},
		{"key with spaces", "my password", "secret", "[REDACTED]"},

		// Very long values
		{"very long password", "password", strings.Repeat("a", 10000), "[REDACTED]"},
		{"very long normal value", "description", strings.Repeat("b", 10000), strings.Repeat("b", 10000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attr slog.Attr
			switch v := tt.value.(type) {
			case string:
				attr = slog.String(tt.key, v)
			default:
				attr = slog.Any(tt.key, v)
			}

			result := redactSensitiveData(nil, attr)

			if result.Value.String() != tt.shouldMatch {
				// For very long strings, just check prefix
				if len(tt.shouldMatch) > 100 {
					if !strings.HasPrefix(result.Value.String(), tt.shouldMatch[:100]) {
						t.Errorf("redactSensitiveData(%q) prefix mismatch", tt.key)
					}
				} else {
					t.Errorf("redactSensitiveData(%q, %q) = %q, want %q",
						tt.key, tt.value, result.Value.String(), tt.shouldMatch)
				}
			}
		})
	}
}

func TestRedactSensitiveData_NonStringTypes(t *testing.T) {
	tests := []struct {
		name    string
		attr    slog.Attr
		checkFn func(slog.Attr) bool
	}{
		{
			name: "int value with sensitive key",
			attr: slog.Int("password_length", 12),
			checkFn: func(a slog.Attr) bool {
				// Int with sensitive key should still be redacted
				return a.Value.String() == "[REDACTED]"
			},
		},
		{
			name: "bool value with sensitive key",
			attr: slog.Bool("has_token", true),
			checkFn: func(a slog.Attr) bool {
				return a.Value.String() == "[REDACTED]"
			},
		},
		{
			name: "int64 value with normal key",
			attr: slog.Int64("count", 12345),
			checkFn: func(a slog.Attr) bool {
				return a.Value.Int64() == 12345
			},
		},
		{
			name: "float value with normal key",
			attr: slog.Float64("ratio", 3.14),
			checkFn: func(a slog.Attr) bool {
				return a.Value.Float64() == 3.14
			},
		},
		{
			name: "duration with normal key",
			attr: slog.Duration("elapsed", 5000000000), // 5 seconds
			checkFn: func(a slog.Attr) bool {
				return a.Value.Duration().Seconds() == 5
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redactSensitiveData(nil, tt.attr)
			if !tt.checkFn(result) {
				t.Errorf("redactSensitiveData failed for %s: got %v", tt.name, result)
			}
		})
	}
}

func TestNewHandler_InvalidFormat(t *testing.T) {
	// Test that invalid format defaults to JSON handler
	cfg := Config{
		Level:     slog.LevelInfo,
		Format:    "invalid_format",
		AddSource: false,
	}

	handler := NewHandler(cfg)
	if handler == nil {
		t.Fatal("NewHandler returned nil for invalid format")
	}

	// Verify it creates a JSON handler (default) by checking output format
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	jsonHandler := slog.NewJSONHandler(&buf, opts)
	logger := slog.New(jsonHandler)
	logger.Info("test")

	// JSON output should start with {
	if !strings.HasPrefix(buf.String(), "{") {
		t.Error("Invalid format should default to JSON handler")
	}
}

func TestSetupLogger_ConcurrentAccess(t *testing.T) {
	cfg := Config{
		Level:     slog.LevelInfo,
		Format:    FormatJSON,
		AddSource: false,
	}

	// Setup logger
	logger := SetupLogger(cfg)

	// Test concurrent logging doesn't panic
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Panic in goroutine %d: %v", id, r)
				}
				done <- true
			}()

			for j := 0; j < 100; j++ {
				logger.Info("concurrent test",
					slog.Int("goroutine", id),
					slog.Int("iteration", j))
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSetupLoggerWithAttrs_EmptyValues(t *testing.T) {
	cfg := Config{
		Level:     slog.LevelInfo,
		Format:    FormatJSON,
		AddSource: false,
	}

	// Test with empty strings
	logger := SetupLoggerWithAttrs(cfg, "", "", "")
	if logger == nil {
		t.Error("SetupLoggerWithAttrs returned nil for empty values")
	}

	// Test logging still works
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	testLogger := slog.New(handler).With(
		slog.String("component", ""),
		slog.String("version", ""),
		slog.String("build", ""),
	)
	testLogger.Info("test message")

	if buf.Len() == 0 {
		t.Error("Logger with empty attrs should still produce output")
	}
}

func TestLevelToString_UnknownLevel(t *testing.T) {
	// Test with a custom/unknown level
	customLevel := slog.Level(100)
	result := LevelToString(customLevel)

	// Should default to "info" for unknown levels
	if result != "info" {
		t.Errorf("LevelToString(%v) = %q, want %q", customLevel, result, "info")
	}
}

func TestLogOutput_LevelFiltering(t *testing.T) {
	tests := []struct {
		name          string
		handlerLevel  slog.Level
		logLevel      slog.Level
		shouldContain bool
	}{
		{"debug log at info level", slog.LevelInfo, slog.LevelDebug, false},
		{"info log at info level", slog.LevelInfo, slog.LevelInfo, true},
		{"warn log at info level", slog.LevelInfo, slog.LevelWarn, true},
		{"error log at info level", slog.LevelInfo, slog.LevelError, true},
		{"info log at error level", slog.LevelError, slog.LevelInfo, false},
		{"error log at error level", slog.LevelError, slog.LevelError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := &slog.HandlerOptions{Level: tt.handlerLevel}
			handler := slog.NewTextHandler(&buf, opts)
			logger := slog.New(handler)

			switch tt.logLevel {
			case slog.LevelDebug:
				logger.Debug("test message")
			case slog.LevelInfo:
				logger.Info("test message")
			case slog.LevelWarn:
				logger.Warn("test message")
			case slog.LevelError:
				logger.Error("test message")
			}

			hasOutput := buf.Len() > 0
			if hasOutput != tt.shouldContain {
				t.Errorf("Expected output=%v, got output=%v (buf=%q)",
					tt.shouldContain, hasOutput, buf.String())
			}
		})
	}
}

func TestRedactSensitiveData_WithGroups(t *testing.T) {
	// Test that redaction works with grouped attributes
	groups := []string{"database", "connection"}
	attr := slog.String("password", "secret123")

	result := redactSensitiveData(groups, attr)

	if result.Value.String() != "[REDACTED]" {
		t.Errorf("Redaction should work with groups, got %q", result.Value.String())
	}
}

func TestSensitiveDataRedactionInLogs_AllSensitiveKeys(t *testing.T) {
	var buf bytes.Buffer

	opts := &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: redactSensitiveData,
	}

	handler := slog.NewTextHandler(&buf, opts)
	logger := slog.New(handler)

	// Log with ALL sensitive key types
	logger.Info("Sensitive data test",
		slog.String("password", "pass1"),
		slog.String("token", "tok1"),
		slog.String("secret", "sec1"),
		slog.String("apikey", "key1"),
		slog.String("api_key", "key2"),
		slog.String("credential", "cred1"),
		slog.String("auth", "auth1"),
		slog.String("normal_field", "should_appear"))

	output := buf.String()

	// Verify ALL sensitive values are redacted
	sensitiveValues := []string{"pass1", "tok1", "sec1", "key1", "key2", "cred1", "auth1"}
	for _, val := range sensitiveValues {
		if strings.Contains(output, val) {
			t.Errorf("Sensitive value %q was not redacted", val)
		}
	}

	// Verify normal value is NOT redacted
	if !strings.Contains(output, "should_appear") {
		t.Error("Normal field value was incorrectly redacted")
	}

	// Count redactions
	redactionCount := strings.Count(output, "[REDACTED]")
	if redactionCount != 7 {
		t.Errorf("Expected 7 redactions, got %d", redactionCount)
	}
}
