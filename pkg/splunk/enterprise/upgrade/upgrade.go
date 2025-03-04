package upgrade

import (
	"context"
	"fmt"
	"time"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/enterprise/metrics"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// UpgradeOptions contains configuration for the upgrade process.
type UpgradeOptions struct {
	WaitForHistoricalSearchDrain bool
	UpgradeDrainTimeout          time.Duration
	// Additional flags (e.g., for search restart on remote failure) can be added here.
}

// PerformUpgradeSteps orchestrates the upgrade process. Note that each step’s completion
// should be reflected in the custom resource status (using a function like UpdateUpgradePhase).
func PerformUpgradeSteps(ctx context.Context, client *splclient.SplunkClient, opts UpgradeOptions) error {
	logger := log.FromContext(ctx)

	// Step 1: Initiate upgrade and record start time.
	logger.Info("Initiating SHC upgrade")
	if err := client.InitShcUpgrade(); err != nil {
		return fmt.Errorf("failed to initiate upgrade: %w", err)
	}
	metrics.UpgradeStartTime.Set(float64(time.Now().Unix()))
	// CR status should be updated to "Initiated" in the controller after this step.

	// Step 2: Set manual detention mode.
	logger.Info("Setting manual detention mode")
	if err := client.SetManualDetentionMode(); err != nil {
		return fmt.Errorf("failed to set manual detention mode: %w", err)
	}
	// CR status should be updated to "DetentionSet"

	// Step 3: Wait for historical searches to finish.
	if opts.WaitForHistoricalSearchDrain {
		logger.Info("Waiting for historical searches to finish")
		if err := WaitForHistoricalSearches(ctx, client, opts.UpgradeDrainTimeout); err != nil {
			logger.Error(err, "Historical search drain timed out")
			return err
		}
		// CR status should be updated to "HistoricalSearchDrainComplete"
	}

	// (Optional: Enable search restart on remote failure if required.)

	// Step 4: Finalize upgrade and record end time.
	logger.Info("Finalizing SHC upgrade")
	if err := client.FinalizeShcUpgrade(); err != nil {
		return fmt.Errorf("failed to finalize upgrade: %w", err)
	}
	metrics.UpgradeEndTime.Set(float64(time.Now().Unix()))
	// CR status should be updated to "Finalized"

	// Step 5: Unset manual detention mode.
	logger.Info("Unsetting manual detention mode")
	if err := client.UnsetManualDetentionMode(); err != nil {
		return fmt.Errorf("failed to unset manual detention mode: %w", err)
	}
	// CR status should be updated to "DetentionUnset"

	return nil
}

// waitForHistoricalSearches polls until the active search count is zero or the timeout is reached.
// TODO FIXME Replace with real logic to query active search counts.
func WaitForHistoricalSearches(ctx context.Context, client *splclient.SplunkClient, timeout time.Duration) error {
	logger := log.FromContext(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			return fmt.Errorf("timed out waiting for historical searches to finish")
		case <-ticker.C:
			// Stub: Replace with actual logic to fetch active search counts.
			activeCount := 0
			logger.Info("Polling active search count", "activeCount", activeCount)
			if activeCount == 0 {
				return nil
			}
		}
	}
}
