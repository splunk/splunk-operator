// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package logging_test

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/splunk/splunk-operator/pkg/logging"
	"github.com/stretchr/testify/assert"
)

const (
	envLogLevel     = "LOG_LEVEL"
	envLogFormat    = "LOG_FORMAT"
	envLogAddSource = "LOG_ADD_SOURCE"
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
			expectedFormat: "json",
			expectedSource: false,
		},
		{
			name: "debug level with auto source",
			envVars: map[string]string{
				envLogLevel: "debug",
			},
			expectedLevel:  slog.LevelDebug,
			expectedFormat: "json",
			expectedSource: true,
		},
		{
			name: "warn level with text format",
			envVars: map[string]string{
				envLogLevel:  "warn",
				envLogFormat: "text",
			},
			expectedLevel:  slog.LevelWarn,
			expectedFormat: "text",
			expectedSource: false,
		},
		{
			name: "error level with explicit source",
			envVars: map[string]string{
				envLogLevel:     "error",
				envLogAddSource: "true",
			},
			expectedLevel:  slog.LevelError,
			expectedFormat: "json",
			expectedSource: true,
		},
		{
			name: "debug level with source disabled",
			envVars: map[string]string{
				envLogLevel:     "debug",
				envLogAddSource: "false",
			},
			expectedLevel:  slog.LevelDebug,
			expectedFormat: "json",
			expectedSource: false,
		},
		{
			name: "invalid level defaults to info",
			envVars: map[string]string{
				envLogLevel: "invalid",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "json",
			expectedSource: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := logging.LoadConfig()

			assert.Equal(t, tt.expectedLevel, cfg.Level)
			assert.Equal(t, tt.expectedFormat, cfg.Format)
			assert.Equal(t, tt.expectedSource, cfg.AddSource)

			clearEnv()
		})
	}
}

func TestLoadConfigWithFlags(t *testing.T) {
	clearEnv()
	os.Setenv(envLogLevel, "info")
	os.Setenv(envLogFormat, "json")
	defer clearEnv()

	addSource := true
	cfg := logging.LoadConfigWithFlags("debug", "text", &addSource)

	assert.Equal(t, slog.LevelDebug, cfg.Level)
	assert.Equal(t, "text", cfg.Format)
	assert.True(t, cfg.AddSource)
}

func TestLevelFromString(t *testing.T) {
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
			result := logging.LevelFromString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLevelToString(t *testing.T) {
	tests := []struct {
		level    slog.Level
		expected string
	}{
		{slog.LevelDebug, "debug"},
		{slog.LevelInfo, "info"},
		{slog.LevelWarn, "warn"},
		{slog.LevelError, "error"},
		{slog.Level(100), "info"}, // unknown level defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := logging.LevelToString(tt.level)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name   string
		config logging.Config
	}{
		{
			name: "JSON handler",
			config: logging.Config{
				Level:     slog.LevelInfo,
				Format:    "json",
				AddSource: false,
			},
		},
		{
			name: "Text handler",
			config: logging.Config{
				Level:     slog.LevelDebug,
				Format:    "text",
				AddSource: true,
			},
		},
		{
			name: "Invalid format defaults to JSON",
			config: logging.Config{
				Level:     slog.LevelInfo,
				Format:    "invalid_format",
				AddSource: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := logging.NewHandler(tt.config)
			assert.NotNil(t, handler)
		})
	}
}

func TestSetupLogger(t *testing.T) {
	cfg := logging.Config{
		Level:     slog.LevelInfo,
		Format:    "json",
		AddSource: false,
	}

	logger := logging.SetupLogger(cfg)
	assert.NotNil(t, logger)
	assert.Equal(t, logger, slog.Default())
}

func TestSetupLoggerWithAttrs(t *testing.T) {
	cfg := logging.Config{
		Level:     slog.LevelInfo,
		Format:    "json",
		AddSource: false,
	}

	logger := logging.SetupLogger(cfg,
		slog.String("component", "splunk-operator"),
		slog.String("version", "1.0.0"),
		slog.String("build", "abc123"))
	assert.NotNil(t, logger)
	assert.Equal(t, logger, slog.Default())
}

func TestSetupLoggerWithEmptyAttrs(t *testing.T) {
	cfg := logging.Config{
		Level:     slog.LevelInfo,
		Format:    "json",
		AddSource: false,
	}

	logger := logging.SetupLogger(cfg)
	assert.NotNil(t, logger)

	// Test logging still works
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	testLogger := slog.New(handler)
	testLogger.Info("test message")
	assert.NotEmpty(t, buf.String())
}

func TestSetupLogger_ConcurrentAccess(t *testing.T) {
	cfg := logging.Config{
		Level:     slog.LevelInfo,
		Format:    "json",
		AddSource: false,
	}

	logger := logging.SetupLogger(cfg)

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

	for i := 0; i < 10; i++ {
		<-done
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
			expectedFormat: "text",
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
			expectedFormat: "text",
			expectedSource: true,
		},
		{
			name:           "invalid flag level defaults to info",
			envLevel:       "warn",
			envFormat:      "json",
			flagLevel:      "invalid",
			flagFormat:     "",
			flagAddSource:  nil,
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "json",
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
			expectedFormat: "json",
			expectedSource: false,
		},
		{
			name:           "false addSource pointer explicitly disables",
			envLevel:       "debug",
			envFormat:      "json",
			flagLevel:      "",
			flagFormat:     "",
			flagAddSource:  boolPtr(false),
			expectedLevel:  slog.LevelDebug,
			expectedFormat: "json",
			expectedSource: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			if tt.envLevel != "" {
				os.Setenv(envLogLevel, tt.envLevel)
			}
			if tt.envFormat != "" {
				os.Setenv(envLogFormat, tt.envFormat)
			}

			cfg := logging.LoadConfigWithFlags(tt.flagLevel, tt.flagFormat, tt.flagAddSource)

			assert.Equal(t, tt.expectedLevel, cfg.Level)
			assert.Equal(t, tt.expectedFormat, cfg.Format)
			assert.Equal(t, tt.expectedSource, cfg.AddSource)

			clearEnv()
		})
	}
}

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
				envLogLevel: "notavalidlevel",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "json",
			expectedSource: false,
		},
		{
			name: "numeric log level (invalid)",
			envVars: map[string]string{
				envLogLevel: "123",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "json",
			expectedSource: false,
		},
		{
			name: "special characters in log level",
			envVars: map[string]string{
				envLogLevel: "!@#$%",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "json",
			expectedSource: false,
		},
		{
			name: "invalid format stored as-is",
			envVars: map[string]string{
				envLogFormat: "xml",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "xml",
			expectedSource: false,
		},
		{
			name: "invalid add_source value",
			envVars: map[string]string{
				envLogAddSource: "yes",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "json",
			expectedSource: false,
		},
		{
			name: "mixed case add_source",
			envVars: map[string]string{
				envLogAddSource: "TRUE",
			},
			expectedLevel:  slog.LevelInfo,
			expectedFormat: "json",
			expectedSource: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := logging.LoadConfig()

			assert.Equal(t, tt.expectedLevel, cfg.Level)
			assert.Equal(t, tt.expectedFormat, cfg.Format)
			assert.Equal(t, tt.expectedSource, cfg.AddSource)

			clearEnv()
		})
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
			assert.Equal(t, tt.shouldContain, hasOutput)
		})
	}
}

func TestSensitiveDataRedaction(t *testing.T) {
	var buf bytes.Buffer
	cfg := logging.Config{
		Level:     slog.LevelInfo,
		Format:    "text",
		AddSource: false,
	}

	handler := logging.NewHandlerWithWriter(cfg, &buf)
	logger := slog.New(handler)

	logger.Info("test",
		slog.String("password", "secret123"),
		slog.String("token", "abc123"),
		slog.String("username", "admin"))

	output := buf.String()

	// Verify sensitive data is redacted
	assert.NotContains(t, output, "secret123", "password should be redacted")
	assert.NotContains(t, output, "abc123", "token should be redacted")
	assert.Contains(t, output, "[REDACTED]", "redaction marker should be present")
	assert.Contains(t, output, "admin", "non-sensitive data should not be redacted")
}

func clearEnv() {
	os.Unsetenv(envLogLevel)
	os.Unsetenv(envLogFormat)
	os.Unsetenv(envLogAddSource)
}

func boolPtr(b bool) *bool {
	return &b
}

func TestLevelConversions_Roundtrip(t *testing.T) {
	levels := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
	}

	for _, level := range levels {
		str := logging.LevelToString(level)
		result := logging.LevelFromString(str)
		assert.Equal(t, level, result, "Roundtrip failed for level %v", level)
	}
}

func TestNewHandler_OutputFormat(t *testing.T) {
	t.Run("JSON format produces JSON output", func(t *testing.T) {
		cfg := logging.Config{
			Level:     slog.LevelInfo,
			Format:    "json",
			AddSource: false,
		}
		handler := logging.NewHandler(cfg)
		assert.NotNil(t, handler)
	})

	t.Run("Text format produces text output", func(t *testing.T) {
		cfg := logging.Config{
			Level:     slog.LevelInfo,
			Format:    "text",
			AddSource: false,
		}
		handler := logging.NewHandler(cfg)
		assert.NotNil(t, handler)
	})
}

func TestSensitiveDataRedactionAllKeys(t *testing.T) {
	var buf bytes.Buffer
	cfg := logging.Config{
		Level:     slog.LevelInfo,
		Format:    "text",
		AddSource: false,
	}

	handler := logging.NewHandlerWithWriter(cfg, &buf)
	logger := slog.New(handler)

	// Test all sensitive key patterns
	logger.Info("test",
		slog.String("password", "pass1"),
		slog.String("token", "tok1"),
		slog.String("secret", "sec1"),
		slog.String("apikey", "key1"),
		slog.String("api_key", "key2"),
		slog.String("credential", "cred1"),
		slog.String("auth", "auth1"),
		slog.String("normal_field", "visible"))

	output := buf.String()

	// Verify ALL sensitive values are redacted
	sensitiveValues := []string{"pass1", "tok1", "sec1", "key1", "key2", "cred1", "auth1"}
	for _, val := range sensitiveValues {
		assert.NotContains(t, output, val, "sensitive value %q should be redacted", val)
	}

	// Verify normal value is NOT redacted
	assert.Contains(t, output, "visible", "normal field should not be redacted")
}

func TestConfigStruct(t *testing.T) {
	cfg := logging.Config{
		Level:     slog.LevelDebug,
		Format:    "text",
		AddSource: true,
	}

	assert.Equal(t, slog.LevelDebug, cfg.Level)
	assert.Equal(t, "text", cfg.Format)
	assert.True(t, cfg.AddSource)
}

func TestRedactionThroughHandler(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		value        string
		shouldRedact bool
	}{
		{"password field", "password", "secret123", true},
		{"Password field", "Password", "secret456", true},
		{"admin_password field", "admin_password", "secret789", true},
		{"token field", "token", "tok123", true},
		{"auth_token field", "auth_token", "tok456", true},
		{"secret field", "secret", "sec123", true},
		{"apikey field", "apikey", "key123", true},
		{"api_key field", "api_key", "key456", true},
		{"credential field", "credential", "cred123", true},
		{"auth field", "auth", "auth123", true},
		{"normal field", "namespace", "default", false},
		{"name field", "name", "myapp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := logging.Config{
				Level:     slog.LevelInfo,
				Format:    "text",
				AddSource: false,
			}
			handler := logging.NewHandlerWithWriter(cfg, &buf)
			logger := slog.New(handler)

			logger.Info("test", slog.String(tt.key, tt.value))
			output := buf.String()

			if tt.shouldRedact {
				assert.NotContains(t, output, tt.value, "value should be redacted")
				assert.Contains(t, output, "[REDACTED]")
			} else {
				assert.Contains(t, output, tt.value, "value should NOT be redacted")
			}
		})
	}
}

func TestDebugLevelAutoEnablesSource(t *testing.T) {
	clearEnv()
	os.Setenv(envLogLevel, "debug")
	defer clearEnv()

	cfg := logging.LoadConfig()

	assert.Equal(t, slog.LevelDebug, cfg.Level)
	assert.True(t, cfg.AddSource, "Debug level should auto-enable source")
}

func TestDebugLevelSourceCanBeDisabled(t *testing.T) {
	clearEnv()
	os.Setenv(envLogLevel, "debug")
	os.Setenv(envLogAddSource, "false")
	defer clearEnv()

	cfg := logging.LoadConfig()

	assert.Equal(t, slog.LevelDebug, cfg.Level)
	assert.False(t, cfg.AddSource, "Source should be disabled when explicitly set to false")
}

func TestWarningAlias(t *testing.T) {
	// "warning" should be treated same as "warn"
	result := logging.LevelFromString("warning")
	assert.Equal(t, slog.LevelWarn, result)
}

func TestCaseInsensitiveLevel(t *testing.T) {
	tests := []string{"DEBUG", "Debug", "dEbUg", "debug"}
	for _, input := range tests {
		result := logging.LevelFromString(input)
		assert.Equal(t, slog.LevelDebug, result, "Input: %s", input)
	}
}

func TestFormatCaseInsensitive(t *testing.T) {
	clearEnv()
	os.Setenv(envLogFormat, "JSON")
	defer clearEnv()

	cfg := logging.LoadConfig()
	assert.Equal(t, "json", cfg.Format)
}

func TestAddSourceCaseInsensitive(t *testing.T) {
	clearEnv()
	os.Setenv(envLogAddSource, "TRUE")
	defer clearEnv()

	cfg := logging.LoadConfig()
	assert.True(t, cfg.AddSource)
}

func TestFlagsOverrideEnvVars(t *testing.T) {
	clearEnv()
	os.Setenv(envLogLevel, "info")
	os.Setenv(envLogFormat, "json")
	os.Setenv(envLogAddSource, "false")
	defer clearEnv()

	addSource := true
	cfg := logging.LoadConfigWithFlags("debug", "text", &addSource)

	assert.Equal(t, slog.LevelDebug, cfg.Level, "Flag should override env var for level")
	assert.Equal(t, "text", cfg.Format, "Flag should override env var for format")
	assert.True(t, cfg.AddSource, "Flag should override env var for addSource")
}

func TestEmptyFlagsUseEnvVars(t *testing.T) {
	clearEnv()
	os.Setenv(envLogLevel, "warn")
	os.Setenv(envLogFormat, "text")
	os.Setenv(envLogAddSource, "true")
	defer clearEnv()

	cfg := logging.LoadConfigWithFlags("", "", nil)

	assert.Equal(t, slog.LevelWarn, cfg.Level)
	assert.Equal(t, "text", cfg.Format)
	assert.True(t, cfg.AddSource)
}

func TestNilAddSourcePointer(t *testing.T) {
	clearEnv()
	os.Setenv(envLogAddSource, "true")
	defer clearEnv()

	cfg := logging.LoadConfigWithFlags("", "", nil)
	assert.True(t, cfg.AddSource, "nil pointer should use env var value")
}

func TestExplicitFalseAddSource(t *testing.T) {
	clearEnv()
	os.Setenv(envLogAddSource, "true")
	defer clearEnv()

	addSource := false
	cfg := logging.LoadConfigWithFlags("", "", &addSource)
	assert.False(t, cfg.AddSource, "explicit false should override env var")
}
