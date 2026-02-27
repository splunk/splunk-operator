// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Package examples demonstrates Platform SDK service discovery patterns.
//
// This example shows:
// - Discovering Splunk instances by type
// - Finding indexer clusters for search head configuration
// - Checking service health and readiness
// - Using discovery in controller logic
package examples

import (
	"context"
	"fmt"

	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/discovery"
)

// DiscoverIndexersExample shows how to find indexer clusters for a search head.
func DiscoverIndexersExample(rctx api.ReconcileContext, searchHeadNamespace string) error {
	logger := rctx.Logger()

	logger.Info("Discovering indexer clusters for search head configuration")

	// Discover all indexer clusters in the same namespace
	indexers, err := rctx.DiscoverSplunk(discovery.SplunkSelector{
		Type:      discovery.SplunkTypeIndexerCluster,
		Namespace: searchHeadNamespace,
	})
	if err != nil {
		return fmt.Errorf("failed to discover indexers: %w", err)
	}

	if len(indexers) == 0 {
		logger.Info("No indexer clusters found, search head will run standalone")
		return nil
	}

	logger.Info("Found indexer clusters", "count", len(indexers))

	// Process each indexer cluster
	for _, indexer := range indexers {
		if indexer.Health == nil || !indexer.Health.Healthy {
			logger.Info("Indexer not healthy, skipping",
				"name", indexer.Name,
				"namespace", indexer.Namespace)
			continue
		}

		logger.Info("Configuring search head to use indexer",
			"indexer", indexer.Name,
			"url", indexer.URL,
			"type", indexer.Type)

		// In real controller, you would:
		// 1. Get cluster manager endpoint from indexer.URL
		// 2. Configure search head to use this indexer cluster
		// 3. Update search head configmap with peer nodes
	}

	return nil
}

// DiscoverLicenseManagerExample shows how to find a license manager.
func DiscoverLicenseManagerExample(rctx api.ReconcileContext, namespace string) (*discovery.SplunkEndpoint, error) {
	logger := rctx.Logger()

	logger.Info("Discovering license manager")

	// Find license managers
	licenseMgrs, err := rctx.DiscoverSplunk(discovery.SplunkSelector{
		Type:      discovery.SplunkTypeLicenseManager,
		Namespace: namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to discover license manager: %w", err)
	}

	if len(licenseMgrs) == 0 {
		logger.Info("No license manager found, using local licensing")
		return nil, nil
	}

	// Use the first healthy license manager
	for _, lm := range licenseMgrs {
		if lm.Health != nil && lm.Health.Healthy {
			logger.Info("Found license manager",
				"name", lm.Name,
				"url", lm.URL)
			return &lm, nil
		}
	}

	logger.Info("No healthy license manager found")
	return nil, nil
}

// DiscoverClusterManagerExample shows how to find a cluster manager for an indexer.
func DiscoverClusterManagerExample(rctx api.ReconcileContext, indexerNamespace string) (*discovery.SplunkEndpoint, error) {
	logger := rctx.Logger()

	logger.V(1).Info("Discovering cluster manager for indexer")

	// Find cluster managers in the same namespace
	clusterMgrs, err := rctx.DiscoverSplunk(discovery.SplunkSelector{
		Type:      discovery.SplunkTypeClusterManager,
		Namespace: indexerNamespace,
		Labels: map[string]string{
			"app.kubernetes.io/part-of": "splunk-enterprise",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to discover cluster manager: %w", err)
	}

	if len(clusterMgrs) == 0 {
		return nil, fmt.Errorf("no cluster manager found for indexer cluster")
	}

	// Verify the cluster manager is ready
	cm := &clusterMgrs[0]
	if cm.Health == nil || !cm.Health.Healthy {
		return nil, fmt.Errorf("cluster manager %s is not healthy", cm.Name)
	}

	logger.Info("Found cluster manager",
		"name", cm.Name,
		"url", cm.URL)

	return cm, nil
}

// DiscoverGenericServicesExample shows how to discover non-Splunk services.
func DiscoverGenericServicesExample(rctx api.ReconcileContext) error {
	logger := rctx.Logger()

	logger.V(1).Info("Discovering external services")

	// Find Kafka services for data ingestion
	kafkaServices, err := rctx.Discover(discovery.Selector{
		Labels: map[string]string{
			"app": "kafka",
		},
		Namespace: "data-platform",
	})
	if err != nil {
		return fmt.Errorf("failed to discover Kafka services: %w", err)
	}

	logger.Info("Found Kafka services", "count", len(kafkaServices))

	for _, svc := range kafkaServices {
		logger.V(1).Info("Kafka service discovered",
			"name", svc.Name,
			"url", svc.URL,
			"namespace", svc.Namespace)

		// Configure Splunk to ingest from this Kafka instance
	}

	return nil
}

// CrossNamespaceDiscoveryExample shows discovering resources across namespaces.
func CrossNamespaceDiscoveryExample(rctx api.ReconcileContext) error {
	logger := rctx.Logger()

	logger.Info("Discovering all Splunk instances across cluster")

	// Discover all standalones (cluster-wide)
	standalones, err := rctx.DiscoverSplunk(discovery.SplunkSelector{
		Type:      discovery.SplunkTypeStandalone,
		Namespace: "", // Empty = all namespaces
	})
	if err != nil {
		return fmt.Errorf("failed to discover standalones: %w", err)
	}

	// Group by namespace
	byNamespace := make(map[string][]discovery.SplunkEndpoint)
	for _, standalone := range standalones {
		byNamespace[standalone.Namespace] = append(
			byNamespace[standalone.Namespace],
			standalone,
		)
	}

	for ns, instances := range byNamespace {
		logger.Info("Standalones in namespace",
			"namespace", ns,
			"count", len(instances))
	}

	return nil
}

// Example usage in a real reconciler:
func ReconcileSearchHeadWithDiscovery(ctx context.Context, rctx api.ReconcileContext) error {
	logger := rctx.Logger()

	// Step 1: Find indexer clusters
	if err := DiscoverIndexersExample(rctx, rctx.Namespace()); err != nil {
		return err
	}

	// Step 2: Find license manager
	licenseMgr, err := DiscoverLicenseManagerExample(rctx, rctx.Namespace())
	if err != nil {
		return err
	}

	if licenseMgr != nil {
		logger.Info("Will configure search head to use license manager",
			"licenseManagerURL", licenseMgr.URL)

		// Configure search head with license manager URL
		// This would be done via ConfigMap or environment variables
	}

	// Step 3: Build search head configuration based on discoveries
	// ... (continue with StatefulSet building using discovered endpoints)

	return nil
}
