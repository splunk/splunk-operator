package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/zapr"
	"github.com/splunk/splunk-operator/e2e/framework/config"
	"github.com/splunk/splunk-operator/e2e/framework/data"
	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	"github.com/splunk/splunk-operator/e2e/framework/logging"
	"github.com/splunk/splunk-operator/e2e/framework/results"
	"github.com/splunk/splunk-operator/e2e/framework/runner"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/steps"
	"github.com/splunk/splunk-operator/e2e/framework/telemetry"
	"go.uber.org/zap"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

func main() {
	cfg := config.Load()
	logger, err := logging.NewLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()
	ctrlLog.SetLogger(zapr.NewLogger(logger))

	telemetryClient, shutdownTelemetry, err := telemetry.Init(context.Background(), cfg, logger)
	if err != nil {
		logger.Fatal("failed to initialize telemetry", zapError(err))
	}
	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			logger.Warn("failed to shutdown telemetry", zapError(err))
		}
	}()

	kube, err := k8s.NewClient(cfg.Kubeconfig)
	if err != nil {
		logger.Fatal("failed to init kube client", zapError(err))
	}

	registry, err := data.LoadRegistry(cfg.DatasetRegistry)
	if err != nil {
		logger.Fatal("failed to load dataset registry", zapError(err))
	}

	stepRegistry := steps.NewRegistry()
	steps.RegisterDefaults(stepRegistry)

	specs, err := spec.LoadSpecs(cfg.SpecDir)
	if err != nil {
		logger.Fatal("failed to load specs", zapError(err))
	}

	runner, err := runner.NewRunner(cfg, logger, stepRegistry, registry, kube, telemetryClient)
	if err != nil {
		logger.Fatal("failed to initialize runner", zapError(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now().UTC()
	result, err := runner.RunAll(ctx, specs)
	if err != nil {
		logger.Fatal("run failed", zapError(err))
	}
	if err := runner.FlushArtifacts(result); err != nil {
		logger.Fatal("failed to write artifacts", zapError(err))
	}

	summary := summarize(result)
	logger.Info("run complete", zapAny("summary", summary), zapDuration("duration", time.Since(start)))
	fmt.Printf("tests: %d passed=%d failed=%d skipped=%d\n", summary.Total, summary.Passed, summary.Failed, summary.Skipped)
}

type summaryStats struct {
	Total   int
	Passed  int
	Failed  int
	Skipped int
}

func summarize(result *results.RunResult) summaryStats {
	stats := summaryStats{Total: len(result.Tests)}
	for _, test := range result.Tests {
		switch test.Status {
		case results.StatusPassed:
			stats.Passed++
		case results.StatusFailed:
			stats.Failed++
		case results.StatusSkipped:
			stats.Skipped++
		}
	}
	return stats
}

func zapError(err error) zap.Field {
	return zap.Error(err)
}

func zapAny(key string, value interface{}) zap.Field {
	return zap.Any(key, value)
}

func zapDuration(key string, value time.Duration) zap.Field {
	return zap.Duration(key, value)
}
