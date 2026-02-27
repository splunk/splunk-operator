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

package builders

import (
	"fmt"

	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/builders"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceBuilder builds Service resources with a fluent API.
type ServiceBuilder struct {
	namespace string
	ownerName string

	// Service fields
	name        string
	serviceType corev1.ServiceType
	ports       []corev1.ServicePort
	selector    map[string]string
	labels      map[string]string
	annotations map[string]string

	// Discovery labels
	addDiscoveryLabels bool
}

// NewServiceBuilder creates a new ServiceBuilder.
func NewServiceBuilder(namespace, ownerName string) *ServiceBuilder {
	return &ServiceBuilder{
		namespace:   namespace,
		ownerName:   ownerName,
		serviceType: corev1.ServiceTypeClusterIP,
		labels:      make(map[string]string),
		annotations: make(map[string]string),
	}
}

// WithName sets the Service name.
func (b *ServiceBuilder) WithName(name string) builders.ServiceBuilder {
	b.name = name
	return b
}

// WithNamespace sets the namespace.
func (b *ServiceBuilder) WithNamespace(namespace string) builders.ServiceBuilder {
	b.namespace = namespace
	return b
}

// WithType sets the service type.
func (b *ServiceBuilder) WithType(serviceType corev1.ServiceType) builders.ServiceBuilder {
	b.serviceType = serviceType
	return b
}

// WithPorts sets the service ports.
func (b *ServiceBuilder) WithPorts(ports []corev1.ServicePort) builders.ServiceBuilder {
	b.ports = ports
	return b
}

// WithSelector sets the pod selector.
func (b *ServiceBuilder) WithSelector(selector map[string]string) builders.ServiceBuilder {
	b.selector = selector
	return b
}

// WithLabels sets labels.
func (b *ServiceBuilder) WithLabels(labels map[string]string) builders.ServiceBuilder {
	for k, v := range labels {
		b.labels[k] = v
	}
	return b
}

// WithAnnotations sets annotations.
func (b *ServiceBuilder) WithAnnotations(annotations map[string]string) builders.ServiceBuilder {
	for k, v := range annotations {
		b.annotations[k] = v
	}
	return b
}

// WithDiscoveryLabels adds standard labels for service discovery.
func (b *ServiceBuilder) WithDiscoveryLabels() builders.ServiceBuilder {
	b.addDiscoveryLabels = true
	return b
}

// Build constructs the Service.
func (b *ServiceBuilder) Build() (*corev1.Service, error) {
	if b.name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(b.ports) == 0 {
		return nil, fmt.Errorf("at least one port is required")
	}

	// Build labels
	labels := b.buildLabels()

	// Build selector - default to same as labels if not specified
	selector := b.selector
	if selector == nil {
		selector = map[string]string{
			"app.kubernetes.io/name":     b.name,
			"app.kubernetes.io/instance": b.ownerName,
		}
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.name,
			Namespace:   b.namespace,
			Labels:      labels,
			Annotations: b.annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     b.serviceType,
			Ports:    b.ports,
			Selector: selector,
		},
	}

	return service, nil
}

// buildLabels constructs the label map.
func (b *ServiceBuilder) buildLabels() map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       b.name,
		"app.kubernetes.io/instance":   b.ownerName,
		"app.kubernetes.io/managed-by": "splunk-operator",
	}

	// Add discovery labels if requested
	if b.addDiscoveryLabels {
		labels["splunk.com/discoverable"] = "true"
	}

	// Merge user labels
	for k, v := range b.labels {
		labels[k] = v
	}

	return labels
}
