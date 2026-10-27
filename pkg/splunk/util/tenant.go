// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"slices"

	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
)

// GetTenantName returns the configured tenant name when namespace is in the
// operator's WATCH_NAMESPACE scope. An empty WATCH_NAMESPACE means all
// namespaces are watched. When the tenant label is absent or empty, the
// namespace name is used.
func GetTenantName(namespace *corev1.Namespace) (string, error) {
	if namespace == nil {
		return "", fmt.Errorf("namespace is nil")
	}

	watchNamespaces := config.GetWatchNamespaces()
	if len(watchNamespaces) > 0 && !slices.Contains(watchNamespaces, namespace.Name) {
		return "", fmt.Errorf("namespace %q is not watched by the operator", namespace.Name)
	}

	if tenantName := namespace.Labels[common.LabelTenant]; tenantName != "" {
		return tenantName, nil
	}
	return namespace.Name, nil
}
