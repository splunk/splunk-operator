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

package discovery

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/discovery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Service implements service discovery for Kubernetes and Splunk resources.
type Service struct {
	client client.Client
	config *api.RuntimeConfig
	logger logr.Logger
}

// NewService creates a new discovery service.
func NewService(client client.Client, config *api.RuntimeConfig, logger logr.Logger) *Service {
	return &Service{
		client: client,
		config: config,
		logger: logger.WithName("discovery"),
	}
}

// DiscoverSplunk finds Splunk instances based on the provided selector.
func (s *Service) DiscoverSplunk(ctx context.Context, selector discovery.SplunkSelector) ([]discovery.SplunkEndpoint, error) {
	logger := s.logger.WithValues("type", selector.Type, "namespace", selector.Namespace)
	logger.V(1).Info("discovering Splunk instances")

	// Build label selector for Splunk services
	labelSelector := s.buildSplunkLabelSelector(selector)

	// List services matching the selector
	serviceList := &corev1.ServiceList{}
	listOpts := []client.ListOption{
		client.MatchingLabelsSelector{Selector: labelSelector},
	}

	// Add namespace constraint if specified
	if selector.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(selector.Namespace))
	} else if !s.config.ClusterScoped {
		// If not cluster-scoped, restrict to configured namespace
		listOpts = append(listOpts, client.InNamespace(s.config.Namespace))
	}

	if err := s.client.List(ctx, serviceList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	logger.V(1).Info("found services", "count", len(serviceList.Items))

	// Convert services to Splunk endpoints
	endpoints := make([]discovery.SplunkEndpoint, 0, len(serviceList.Items))
	for _, svc := range serviceList.Items {
		endpoint, err := s.serviceToSplunkEndpoint(ctx, svc, selector)
		if err != nil {
			logger.Error(err, "failed to convert service to endpoint", "service", svc.Name)
			continue
		}
		if endpoint != nil {
			endpoints = append(endpoints, *endpoint)
		}
	}

	logger.V(1).Info("discovered Splunk endpoints", "count", len(endpoints))
	return endpoints, nil
}

// Discover finds generic Kubernetes services.
func (s *Service) Discover(ctx context.Context, selector discovery.Selector) ([]discovery.Endpoint, error) {
	logger := s.logger.WithValues("namespace", selector.Namespace)
	logger.V(1).Info("discovering services")

	// Build label selector
	labelSelector, err := labels.ValidatedSelectorFromSet(selector.Labels)
	if err != nil {
		return nil, fmt.Errorf("invalid label selector: %w", err)
	}

	// List services
	serviceList := &corev1.ServiceList{}
	listOpts := []client.ListOption{
		client.MatchingLabelsSelector{Selector: labelSelector},
	}

	if selector.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(selector.Namespace))
	} else if !s.config.ClusterScoped {
		listOpts = append(listOpts, client.InNamespace(s.config.Namespace))
	}

	if err := s.client.List(ctx, serviceList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	logger.V(1).Info("found services", "count", len(serviceList.Items))

	// Convert services to endpoints
	endpoints := make([]discovery.Endpoint, 0, len(serviceList.Items))
	for _, svc := range serviceList.Items {
		endpoint := s.serviceToEndpoint(svc, selector)
		if endpoint != nil {
			endpoints = append(endpoints, *endpoint)
		}
	}

	logger.V(1).Info("discovered endpoints", "count", len(endpoints))
	return endpoints, nil
}

// buildSplunkLabelSelector builds a label selector for Splunk services.
func (s *Service) buildSplunkLabelSelector(selector discovery.SplunkSelector) labels.Selector {
	// Base labels for Splunk resources managed by the operator
	baseLabels := map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  string(selector.Type),
	}

	// Merge with additional labels
	for k, v := range selector.Labels {
		baseLabels[k] = v
	}

	labelSelector, _ := labels.ValidatedSelectorFromSet(baseLabels)
	return labelSelector
}

// serviceToSplunkEndpoint converts a Kubernetes service to a Splunk endpoint.
func (s *Service) serviceToSplunkEndpoint(ctx context.Context, svc corev1.Service, selector discovery.SplunkSelector) (*discovery.SplunkEndpoint, error) {
	// Find management port (default 8089)
	var mgmtPort int32 = 8089

	for _, port := range svc.Spec.Ports {
		if port.Name == "mgmt" || port.Name == "management" {
			mgmtPort = port.Port
			break
		}
	}

	// Check for TLS configuration in annotations
	scheme := "https"
	if tlsDisabled, ok := svc.Annotations["splunk.com/tls-disabled"]; ok {
		if disabled, _ := strconv.ParseBool(tlsDisabled); disabled {
			scheme = "http"
		}
	}

	// Build endpoint URL
	host := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
	url := fmt.Sprintf("%s://%s:%d", scheme, host, mgmtPort)

	// Check if endpoint is ready
	healthy := s.isServiceReady(ctx, svc)

	var health *discovery.HealthStatus
	if healthy {
		health = &discovery.HealthStatus{
			Healthy: true,
		}
	}

	endpoint := discovery.SplunkEndpoint{
		Name:       svc.Name,
		Type:       selector.Type,
		URL:        url,
		IsExternal: false,
		Namespace:  svc.Namespace,
		Health:     health,
		Labels:     svc.Labels,
	}

	return &endpoint, nil
}

// serviceToEndpoint converts a Kubernetes service to a generic endpoint.
func (s *Service) serviceToEndpoint(svc corev1.Service, selector discovery.Selector) *discovery.Endpoint {
	// Find the primary port
	var port int32
	if len(svc.Spec.Ports) > 0 {
		// Use the first port by default
		port = svc.Spec.Ports[0].Port
	}

	scheme := "http"
	if port == 443 || svc.Annotations["service.alpha.kubernetes.io/scheme"] == "https" {
		scheme = "https"
	}

	host := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
	url := fmt.Sprintf("%s://%s:%d", scheme, host, port)

	return &discovery.Endpoint{
		Name:      svc.Name,
		URL:       url,
		Namespace: svc.Namespace,
		Labels:    svc.Labels,
		Health: &discovery.HealthStatus{
			Healthy: true, // Generic services are assumed ready
		},
	}
}

// isServiceReady checks if a service has ready endpoints.
func (s *Service) isServiceReady(ctx context.Context, svc corev1.Service) bool {
	// For headless services, check if there are any pods
	if svc.Spec.ClusterIP == "None" {
		podList := &corev1.PodList{}
		listOpts := []client.ListOption{
			client.InNamespace(svc.Namespace),
			client.MatchingLabels(svc.Spec.Selector),
		}

		if err := s.client.List(ctx, podList, listOpts...); err != nil {
			s.logger.Error(err, "failed to list pods for service", "service", svc.Name)
			return false
		}

		// Check if at least one pod is ready
		for _, pod := range podList.Items {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}

	// For regular services, assume ready if ClusterIP is assigned
	return svc.Spec.ClusterIP != ""
}
