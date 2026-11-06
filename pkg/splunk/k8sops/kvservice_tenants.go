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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoverKVServiceTenants resolves the same Namespace metadata used by Enterprise clients.
// Only watched, non-terminating namespaces selecting this backend participate.
func DiscoverKVServiceTenants(ctx context.Context, c client.Reader, cr *enterpriseApi.KVService) (map[string]string, error) {
	var namespaces corev1.NamespaceList
	if err := c.List(ctx, &namespaces, client.MatchingLabels{
		splcommon.LabelKVService: splcommon.LabelKVServiceEnabled,
	}); err != nil {
		return nil, fmt.Errorf("discover KVService tenant namespaces: %w", err)
	}
	mapping := make(map[string]string)
	invalid := func(message string, cause error) (map[string]string, error) {
		return nil, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, message, cause)
	}
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
		if err != nil {
			continue
		}
		if endpoint.KVServiceName != cr.Name || endpoint.Namespace != cr.Namespace {
			continue
		}
		if err := splutil.ValidateKVServiceTenantName(tenant); err != nil {
			return invalid(
				fmt.Sprintf("Namespace %q has an invalid tenant name. Set label %s to a name matching ^[a-z][a-z0-9]{0,29}$.", namespace.Name, splcommon.LabelTenant),
				fmt.Errorf("validate tenant %q from namespace %q for KVService %s/%s: %w", tenant, namespace.Name, cr.Namespace, cr.Name, err))
		}
		if previous, exists := mapping[tenant]; exists {
			return invalid(
				fmt.Sprintf("Tenant %q is selected by both namespaces %q and %q. Assign a unique %s label to each namespace selecting this backend.", tenant, previous, namespace.Name, splcommon.LabelTenant),
				fmt.Errorf("duplicate tenant %q discovered in namespaces %q and %q for KVService %s/%s", tenant, previous, namespace.Name, cr.Namespace, cr.Name))
		}
		mapping[tenant] = namespace.Name
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

// SuspendKVService scales the owned KVService Deployment to zero replicas.
// Databases and the Service remain untouched.
func SuspendKVService(ctx context.Context, c client.Client, cr *enterpriseApi.KVService) error {
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Namespace: cr.Namespace, Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.Name)}
	if err := c.Get(ctx, key, deployment); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("get KVService Deployment %s for suspension: %w", key, err)
	}
	if owner := metav1.GetControllerOf(deployment); owner == nil || owner.UID != cr.UID {
		return fmt.Errorf("cannot suspend KVService Deployment %s: not owned by this KVService", key)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 0 {
		zero := int32(0)
		deployment.Spec.Replicas = &zero
		if err := c.Update(ctx, deployment); err != nil {
			return fmt.Errorf("suspend KVService Deployment %s: %w", key, err)
		}
		return nil
	}
	return nil
}
