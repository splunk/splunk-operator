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

// Package discovery provides types for service discovery.
package discovery

import "time"

// SplunkSelector specifies criteria for discovering Splunk instances.
type SplunkSelector struct {
	// Type of Splunk instance to find.
	Type SplunkType

	// Namespace to search (empty = all namespaces in cluster-scoped mode).
	Namespace string

	// Labels to match (optional, additional filtering).
	Labels map[string]string

	// IncludeExternal includes ExternalSplunkCluster resources.
	IncludeExternal bool
}

// SplunkType identifies the type of Splunk instance.
type SplunkType string

const (
	SplunkTypeStandalone        SplunkType = "Standalone"
	SplunkTypeSearchHeadCluster SplunkType = "SearchHeadCluster"
	SplunkTypeIndexerCluster    SplunkType = "IndexerCluster"
	SplunkTypeClusterManager    SplunkType = "ClusterManager"
	SplunkTypeLicenseManager    SplunkType = "LicenseManager"
	SplunkTypeMonitoringConsole SplunkType = "MonitoringConsole"
)

// SplunkEndpoint represents a discovered Splunk instance.
type SplunkEndpoint struct {
	// Name of the Splunk instance.
	Name string

	// Type of Splunk instance.
	Type SplunkType

	// URL to connect (e.g., https://splunk-shc.default.svc:8089).
	URL string

	// IsExternal indicates if this is an external (non-Kubernetes) instance.
	IsExternal bool

	// Namespace (for Kubernetes instances).
	Namespace string

	// Health status.
	Health *HealthStatus

	// Labels on the resource.
	Labels map[string]string
}

// Selector specifies criteria for discovering generic Kubernetes services.
type Selector struct {
	// Namespace to search (empty = all namespaces).
	Namespace string

	// Labels to match.
	Labels map[string]string

	// ServiceType to filter (optional).
	ServiceType string
}

// Endpoint represents a discovered Kubernetes service.
type Endpoint struct {
	// Name of the service.
	Name string

	// URL to connect.
	URL string

	// Namespace.
	Namespace string

	// Labels on the service.
	Labels map[string]string

	// Health status.
	Health *HealthStatus
}

// HealthStatus indicates the health of an endpoint.
type HealthStatus struct {
	// Healthy indicates if the service is reachable and responding.
	Healthy bool

	// LastChecked timestamp.
	LastChecked time.Time

	// Error message if not healthy.
	Error string

	// ResponseTime for the health check.
	ResponseTime time.Duration
}
