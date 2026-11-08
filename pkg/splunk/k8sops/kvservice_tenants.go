// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sops

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TenantDiscoveryError reports namespace-local policy failures alongside valid membership.
// Kubernetes read failures are returned separately and preserve the last good publication.
type TenantDiscoveryError struct {
	Blocked []enterpriseApi.KVServiceTenantStatus
}

func (e *TenantDiscoveryError) Error() string {
	messages := make([]string, 0, len(e.Blocked))
	for _, tenant := range e.Blocked {
		messages = append(messages, tenant.Message)
	}
	return strings.Join(messages, "; ")
}

// DiscoverKVServiceTenants excludes every participant in a duplicate assignment;
// an invalid namespace never suppresses discovery of unrelated healthy tenants.
func DiscoverKVServiceTenants(ctx context.Context, c client.Reader, cr *enterpriseApi.KVService) (map[string]string, error) {
	var namespaces corev1.NamespaceList
	if err := c.List(ctx, &namespaces, client.MatchingLabels{splcommon.LabelKVService: splcommon.LabelKVServiceEnabled}); err != nil {
		return nil, fmt.Errorf("discover KVService tenant namespaces: %w", err)
	}
	groups := make(map[string][]string)
	blocked := &TenantDiscoveryError{}
	for i := range namespaces.Items {
		namespace := &namespaces.Items[i]
		if namespace.DeletionTimestamp != nil {
			continue
		}
		tenant, err := splutil.GetTenantName(namespace)
		if err != nil {
			continue
		} // Excludes namespaces outside WATCH_NAMESPACE.
		endpoint, err := namespaceKVServiceAddress(ctx, namespace)
		if err != nil || endpoint.KVServiceName != cr.Name || endpoint.Namespace != cr.Namespace {
			continue
		}
		if err := splutil.ValidateKVServiceTenantName(tenant); err != nil {
			blocked.Blocked = append(blocked.Blocked, enterpriseApi.KVServiceTenantStatus{Name: tenant, Namespace: namespace.Name, Reason: "InvalidTenantName", Message: fmt.Sprintf("Set namespace %q label %s to a unique supported tenant name: %v", namespace.Name, splcommon.LabelTenant, err)})
			continue
		}
		groups[tenant] = append(groups[tenant], namespace.Name)
	}
	mapping := make(map[string]string)
	for _, tenant := range slices.Sorted(maps.Keys(groups)) {
		names := groups[tenant]
		slices.Sort(names)
		if len(names) == 1 {
			mapping[tenant] = names[0]
			continue
		}
		for _, namespace := range names {
			blocked.Blocked = append(blocked.Blocked, enterpriseApi.KVServiceTenantStatus{Name: tenant, Namespace: namespace, Reason: "DuplicateTenant", Message: fmt.Sprintf("Tenant %q is selected by namespaces %q. Assign a unique %s label to each namespace selecting this backend.", tenant, names, splcommon.LabelTenant)})
		}
	}
	slices.SortFunc(blocked.Blocked, func(a, b enterpriseApi.KVServiceTenantStatus) int { return strings.Compare(a.Namespace, b.Namespace) })
	if len(blocked.Blocked) > 0 {
		return mapping, blocked
	}
	return mapping, nil
}

// ApplyKVServiceTenantConfig publishes discovered membership for the existing
// KVService runtime protocol. This generated object is never a discovery input.
func ApplyKVServiceTenantConfig(ctx context.Context, c client.Client, cr *enterpriseApi.KVService, mapping map[string]string) (*corev1.ConfigMap, error) {
	desired := resources.NewKVServiceTenantConfig(cr, mapping)
	existing := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired), existing); apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			return nil, fmt.Errorf("create tenant membership ConfigMap %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return desired, nil
	} else if err != nil {
		return nil, fmt.Errorf("get tenant membership ConfigMap %s/%s: %w", cr.Namespace, desired.Name, err)
	}
	if owner := metav1.GetControllerOf(existing); owner == nil || owner.UID != cr.UID {
		return nil, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed,
			fmt.Sprintf("Tenant runtime ConfigMap %s/%s is not owned by KVService %s/%s. Resolve the ConfigMap ownership conflict before retrying.", existing.Namespace, existing.Name, cr.Namespace, cr.Name),
			fmt.Errorf("tenant membership ConfigMap %s/%s has controller owner %v; expected KVService UID %s", existing.Namespace, existing.Name, owner, cr.UID))
	}
	if !maps.Equal(existing.Data, desired.Data) {
		existing.Data = maps.Clone(desired.Data)
		if err := c.Update(ctx, existing); err != nil {
			return nil, fmt.Errorf("update tenant membership ConfigMap %s/%s: %w", existing.Namespace, existing.Name, err)
		}
	}
	return existing, nil
}
