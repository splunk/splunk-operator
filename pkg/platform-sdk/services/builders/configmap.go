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

// ConfigMapBuilder builds ConfigMap resources with a fluent API.
type ConfigMapBuilder struct {
	namespace string
	ownerName string

	// ConfigMap fields
	name        string
	data        map[string]string
	binaryData  map[string][]byte
	labels      map[string]string
	annotations map[string]string
}

// NewConfigMapBuilder creates a new ConfigMapBuilder.
func NewConfigMapBuilder(namespace, ownerName string) *ConfigMapBuilder {
	return &ConfigMapBuilder{
		namespace:   namespace,
		ownerName:   ownerName,
		data:        make(map[string]string),
		binaryData:  make(map[string][]byte),
		labels:      make(map[string]string),
		annotations: make(map[string]string),
	}
}

// WithName sets the ConfigMap name.
func (b *ConfigMapBuilder) WithName(name string) builders.ConfigMapBuilder {
	b.name = name
	return b
}

// WithNamespace sets the namespace.
func (b *ConfigMapBuilder) WithNamespace(namespace string) builders.ConfigMapBuilder {
	b.namespace = namespace
	return b
}

// WithData sets string data.
func (b *ConfigMapBuilder) WithData(data map[string]string) builders.ConfigMapBuilder {
	for k, v := range data {
		b.data[k] = v
	}
	return b
}

// WithBinaryData sets binary data.
func (b *ConfigMapBuilder) WithBinaryData(binaryData map[string][]byte) builders.ConfigMapBuilder {
	for k, v := range binaryData {
		b.binaryData[k] = v
	}
	return b
}

// WithLabels sets labels.
func (b *ConfigMapBuilder) WithLabels(labels map[string]string) builders.ConfigMapBuilder {
	for k, v := range labels {
		b.labels[k] = v
	}
	return b
}

// WithAnnotations sets annotations.
func (b *ConfigMapBuilder) WithAnnotations(annotations map[string]string) builders.ConfigMapBuilder {
	for k, v := range annotations {
		b.annotations[k] = v
	}
	return b
}

// Build constructs the ConfigMap.
func (b *ConfigMapBuilder) Build() (*corev1.ConfigMap, error) {
	if b.name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(b.data) == 0 && len(b.binaryData) == 0 {
		return nil, fmt.Errorf("at least one data entry is required")
	}

	// Build labels
	labels := b.buildLabels()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.name,
			Namespace:   b.namespace,
			Labels:      labels,
			Annotations: b.annotations,
		},
		Data:       b.data,
		BinaryData: b.binaryData,
	}

	return configMap, nil
}

// buildLabels constructs the label map.
func (b *ConfigMapBuilder) buildLabels() map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       b.name,
		"app.kubernetes.io/instance":   b.ownerName,
		"app.kubernetes.io/managed-by": "splunk-operator",
	}

	// Merge user labels
	for k, v := range b.labels {
		labels[k] = v
	}

	return labels
}
