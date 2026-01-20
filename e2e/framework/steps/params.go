package steps

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func getString(params map[string]interface{}, key string, fallback string) string {
	if params == nil {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return fallback
		}
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func getInt(params map[string]interface{}, key string, fallback int) int {
	if params == nil {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed := fallback
		_, err := fmt.Sscanf(typed, "%d", &parsed)
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

func getDuration(params map[string]interface{}, key string, fallback time.Duration) time.Duration {
	if params == nil {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case time.Duration:
		return typed
	case string:
		parsed, err := time.ParseDuration(typed)
		if err != nil {
			return fallback
		}
		return parsed
	case int:
		return time.Duration(typed) * time.Second
	case int64:
		return time.Duration(typed) * time.Second
	case float64:
		return time.Duration(typed * float64(time.Second))
	default:
		return fallback
	}
}

func getBool(params map[string]interface{}, key string, fallback bool) bool {
	if params == nil {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "y":
			return true
		case "false", "0", "no", "n":
			return false
		}
	}
	return fallback
}

func expandVars(value string, vars map[string]string) string {
	if value == "" || vars == nil {
		return value
	}
	return os.Expand(value, func(key string) string {
		if replacement, ok := vars[key]; ok {
			return replacement
		}
		return os.Getenv(key)
	})
}

func expandStringSlice(values []string, vars map[string]string) []string {
	if len(values) == 0 || vars == nil {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, expandVars(trimmed, vars))
	}
	return out
}

func getStringFallback(stepParams map[string]interface{}, specParams map[string]string, key string, fallback string) string {
	if value := getString(stepParams, key, ""); value != "" {
		return value
	}
	if specParams != nil {
		if value := strings.TrimSpace(specParams[key]); value != "" {
			return value
		}
	}
	return fallback
}

func getIntFallback(stepParams map[string]interface{}, specParams map[string]string, key string, fallback int) int {
	if value := getInt(stepParams, key, fallback); value != fallback {
		return value
	}
	if specParams == nil {
		return fallback
	}
	raw := strings.TrimSpace(specParams[key])
	if raw == "" {
		return fallback
	}
	parsed := fallback
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func getBoolFallback(stepParams map[string]interface{}, specParams map[string]string, key string, fallback bool) bool {
	if stepParams != nil {
		if value, ok := stepParams[key]; ok && value != nil {
			switch typed := value.(type) {
			case bool:
				return typed
			case string:
				switch strings.ToLower(strings.TrimSpace(typed)) {
				case "true", "1", "yes", "y":
					return true
				case "false", "0", "no", "n":
					return false
				}
			default:
				return fallback
			}
		}
	}
	if specParams != nil {
		if raw := strings.TrimSpace(specParams[key]); raw != "" {
			switch strings.ToLower(raw) {
			case "true", "1", "yes", "y":
				return true
			case "false", "0", "no", "n":
				return false
			}
		}
	}
	return fallback
}
