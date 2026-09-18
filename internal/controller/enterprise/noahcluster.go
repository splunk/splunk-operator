// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// NoahCluster is configuration-only and has no controller of its own. These
// permissions exist for other resources controllers, which watch NoahCluster and read it while resolving their Noah connection.
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=noahclusters,verbs=get;list;watch

// noahClusterNamesForSecret returns the names of the same-namespace
// NoahClusters whose authSecretRef points at secret.
func noahClusterNamesForSecret(ctx context.Context, reader client.Reader, secret *corev1.Secret) (map[string]struct{}, error) {
	var noahClusters enterpriseApi.NoahClusterList
	if err := reader.List(ctx, &noahClusters, client.InNamespace(secret.Namespace)); err != nil {
		return nil, fmt.Errorf("list NoahClusters in namespace %s for Secret %s: %w", secret.Namespace, secret.Name, err)
	}

	names := make(map[string]struct{})
	for i := range noahClusters.Items {
		noahCluster := &noahClusters.Items[i]
		if noahCluster.Spec.AuthSecretRef.Name == secret.Name {
			names[noahCluster.Name] = struct{}{}
		}
	}
	return names, nil
}

// noahReferenceRequests maps the workloads whose Noah reference appears in
// matchingNames to reconcile requests. fields reports one workload's key and
// the NoahCluster name it references.
func noahReferenceRequests[T any](items []T, matchingNames map[string]struct{}, fields func(*T) (types.NamespacedName, string)) []reconcile.Request {
	requests := make([]reconcile.Request, 0)
	for i := range items {
		key, referenceName := fields(&items[i])
		if _, found := matchingNames[referenceName]; found {
			requests = append(requests, reconcile.Request{NamespacedName: key})
		}
	}
	return requests
}
