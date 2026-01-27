package logging

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/splunk/splunk-operator/e2e/framework/config"
)

// NewLogger builds a zap logger based on runner config.
func NewLogger(cfg *config.Config) (*zap.Logger, error) {
	var zapCfg zap.Config
	if strings.EqualFold(cfg.LogFormat, "console") {
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	} else {
		zapCfg = zap.NewProductionConfig()
		zapCfg.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	}

	level := strings.ToLower(cfg.LogLevel)
	switch level {
	case "debug":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "warn":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapCfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	return zapCfg.Build()
}
