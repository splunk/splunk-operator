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
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	EnvLogLevel     = "LOG_LEVEL"
	EnvLogFormat    = "LOG_FORMAT"
	EnvLogAddSource = "LOG_ADD_SOURCE"

	FormatJSON = "json"
	FormatText = "text"

	DefaultLogLevel  = slog.LevelInfo
	DefaultLogFormat = FormatJSON
)

// Config holds the logging configuration
type Config struct {
	Level     slog.Level
	Format    string
	AddSource bool
}

// sensitiveKeys contains field names that should be redacted
var sensitiveKeys = []string{
	"password",
	"token",
	"secret",
	"apikey",
	"api_key",
	"credential",
	"auth",
}

// LoadConfig loads logging configuration from environment variables
// Environment variables take precedence over defaults
func LoadConfig() Config {
	cfg := Config{
		Level:     DefaultLogLevel,
		Format:    DefaultLogFormat,
		AddSource: false,
	}

	if level := os.Getenv(EnvLogLevel); level != "" {
		cfg.Level = parseLevel(level)
	}

	if format := os.Getenv(EnvLogFormat); format != "" {
		cfg.Format = strings.ToLower(format)
	}

	if addSource := os.Getenv(EnvLogAddSource); addSource != "" {
		cfg.AddSource = strings.ToLower(addSource) == "true"
	}

	if cfg.Level == slog.LevelDebug && os.Getenv(EnvLogAddSource) == "" {
		cfg.AddSource = true
	}

	return cfg
}

// LoadConfigWithFlags loads configuration with command-line flag overrides
// Flags take precedence over environment variables
func LoadConfigWithFlags(levelFlag, formatFlag string, addSourceFlag *bool) Config {
	cfg := LoadConfig()

	if levelFlag != "" {
		cfg.Level = parseLevel(levelFlag)
	}

	if formatFlag != "" {
		cfg.Format = strings.ToLower(formatFlag)
	}

	if addSourceFlag != nil {
		cfg.AddSource = *addSourceFlag
	}

	if cfg.Level == slog.LevelDebug && addSourceFlag == nil && os.Getenv(EnvLogAddSource) == "" {
		cfg.AddSource = true
	}

	return cfg
}

// parseLevel converts a string to slog.Level
func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewHandler creates a new slog.Handler based on the configuration
func NewHandler(cfg Config) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:       cfg.Level,
		AddSource:   cfg.AddSource,
		ReplaceAttr: redactSensitiveData,
	}

	if cfg.Format == FormatText {
		return slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.NewJSONHandler(os.Stdout, opts)
}

// NewProductionHandler creates a handler optimized for production
// JSON format, INFO level, no source location, sensitive data redaction
func NewProductionHandler() slog.Handler {
	opts := &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		AddSource:   false,
		ReplaceAttr: redactSensitiveData,
	}

	return slog.NewJSONHandler(os.Stdout, opts)
}

// NewDevelopmentHandler creates a handler optimized for development
// Text format, DEBUG level, source location enabled
func NewDevelopmentHandler() slog.Handler {
	opts := &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		AddSource:   true,
		ReplaceAttr: redactSensitiveData,
	}

	return slog.NewTextHandler(os.Stdout, opts)
}

// redactSensitiveData replaces sensitive field values with [REDACTED]
func redactSensitiveData(groups []string, a slog.Attr) slog.Attr {
	keyLower := strings.ToLower(a.Key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(keyLower, sensitive) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}

	if a.Key == slog.TimeKey {
		if t, ok := a.Value.Any().(time.Time); ok {
			return slog.String(slog.TimeKey, t.Format(time.RFC3339))
		}
	}

	return a
}

// SetupLogger initializes the global slog logger with the given configuration
// and returns the configured logger
func SetupLogger(cfg Config) *slog.Logger {
	handler := NewHandler(cfg)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// SetupLoggerWithAttrs initializes the global slog logger with common attributes
func SetupLoggerWithAttrs(cfg Config, component, version, build string) *slog.Logger {
	handler := NewHandler(cfg)
	logger := slog.New(handler)
	logger = logger.With(
		slog.String("component", component),
		slog.String("version", version),
		slog.String("build", build),
	)
	slog.SetDefault(logger)
	return logger
}

// LevelFromString converts a string to slog.Level (exported for use in flags)
func LevelFromString(s string) slog.Level {
	return parseLevel(s)
}

// LevelToString converts slog.Level to string
func LevelToString(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "info"
	}
}
