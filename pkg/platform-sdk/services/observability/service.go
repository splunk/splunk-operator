// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package observability

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	configpkg "github.com/splunk/splunk-operator/pkg/platform-sdk/services/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigResolver interface to avoid import cycle.
type ConfigResolver interface {
	ResolveObservabilityConfig(ctx context.Context, namespace string) (*configpkg.ResolvedObservabilityConfig, error)
}

// Service implements observability functionality for Splunk resources.
type Service struct {
	client         client.Client
	config         *api.RuntimeConfig
	configResolver ConfigResolver
	logger         logr.Logger
}

// NewService creates a new observability service.
func NewService(client client.Client, configResolver ConfigResolver, config *api.RuntimeConfig, logger logr.Logger) *Service {
	return &Service{
		client:         client,
		config:         config,
		configResolver: configResolver,
		logger:         logger.WithName("observability"),
	}
}

// ShouldAddObservability checks if observability should be added for a namespace.
func (s *Service) ShouldAddObservability(ctx context.Context, namespace string) (bool, error) {
	logger := s.logger.WithValues("namespace", namespace)
	logger.V(1).Info("checking if observability should be added")

	// Get observability configuration
	obsConfig, err := s.configResolver.ResolveObservabilityConfig(ctx, namespace)
	if err != nil {
		return false, fmt.Errorf("failed to resolve observability config: %w", err)
	}

	// Check if observability is enabled
	enabled := obsConfig.Enabled

	logger.V(1).Info("observability check complete", "enabled", enabled)
	return enabled, nil
}

// GetObservabilityAnnotations returns the annotations to add for observability.
func (s *Service) GetObservabilityAnnotations(ctx context.Context, namespace string) (map[string]string, error) {
	logger := s.logger.WithValues("namespace", namespace)
	logger.V(1).Info("getting observability annotations")

	// Get observability configuration
	obsConfig, err := s.configResolver.ResolveObservabilityConfig(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve observability config: %w", err)
	}

	// If not enabled, return empty annotations
	if !obsConfig.Enabled {
		return map[string]string{}, nil
	}

	annotations := make(map[string]string)

	// Add Prometheus annotations for metrics
	if obsConfig.PrometheusPort > 0 {
		annotations["prometheus.io/scrape"] = "true"
		annotations["prometheus.io/port"] = fmt.Sprintf("%d", obsConfig.PrometheusPort)

		if obsConfig.PrometheusPath != "" {
			annotations["prometheus.io/path"] = obsConfig.PrometheusPath
		} else {
			annotations["prometheus.io/path"] = "/metrics"
		}
	}

	// Add OpenTelemetry annotations if OTel Collector is enabled
	if obsConfig.Provider == "otel" || obsConfig.OTelCollectorMode != "" {
		// Inject sidecar or daemonset mode
		if obsConfig.OTelCollectorMode == "sidecar" {
			annotations["sidecar.opentelemetry.io/inject"] = "true"
		}

		// Add sampling rate if specified
		if obsConfig.SamplingRate > 0 {
			annotations["opentelemetry.io/sampling-rate"] = fmt.Sprintf("%.2f", obsConfig.SamplingRate)
		}
	}

	logger.V(1).Info("observability annotations generated", "count", len(annotations))
	return annotations, nil
}
