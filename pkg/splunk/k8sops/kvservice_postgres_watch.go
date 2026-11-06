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
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KVServiceDatabaseDependencyIndex indexes published ConfigMap and credential references.
const KVServiceDatabaseDependencyIndex = "kvservice.postgresDatabaseDependencies"

// KVServiceDatabaseDependencies supplies cache and fake-client field indexes.
func KVServiceDatabaseDependencies(obj client.Object) []string {
	database, ok := obj.(*platformApi.PostgresDatabase)
	if !ok || len(databaseKVServiceRequests(database)) == 0 {
		return nil
	}
	var keys []string
	for _, info := range database.Status.Databases {
		if info.ConfigMapRef != nil {
			keys = append(keys, "configmap/"+info.ConfigMapRef.Name)
		}
		if info.AdminUserSecretRef != nil {
			keys = append(keys, "secret/"+info.AdminUserSecretRef.Name)
		}
	}
	return keys
}

// KVServicePostgresMapper maps dependency events without repeating tenant discovery.
type KVServicePostgresMapper struct{ Client client.Client }

// ListKVServices centralizes cached KVService listing and preserves API errors for callers.
func ListKVServices(ctx context.Context, c client.Reader, opts ...client.ListOption) ([]enterpriseApi.KVService, error) {
	var list enterpriseApi.KVServiceList
	if err := c.List(ctx, &list, opts...); err != nil {
		return nil, fmt.Errorf("list KVServices: %w", err)
	}
	return list.Items, nil
}

// databaseKVServiceRequests uses retained management metadata; no GC owner is required.
func databaseKVServiceRequests(database *platformApi.PostgresDatabase) []reconcile.Request {
	name, ok := strings.CutPrefix(database.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation], database.Namespace+"/")
	if !ok || name == "" || strings.Contains(name, "/") {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: database.Namespace, Name: name}}}
}

// MapDatabase wakes consumers even when an unmanaged/foreign collision is repaired.
// Listing the small hosting-namespace consumer set avoids tenant discovery and
// ensures metadata-only routing cannot strand a terminally blocked service.
func (m *KVServicePostgresMapper) MapDatabase(ctx context.Context, obj client.Object) []reconcile.Request {
	database, ok := obj.(*platformApi.PostgresDatabase)
	if !ok {
		return nil
	}
	requests := databaseKVServiceRequests(database)
	services, err := ListKVServices(ctx, m.Client, client.InNamespace(database.Namespace))
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "could not map tenant database consumers", "namespace", database.Namespace, "error", err)
		return requests
	}
	seen := make(map[client.ObjectKey]bool)
	for _, request := range requests {
		seen[request.NamespacedName] = true
	}
	for i := range services {
		key := client.ObjectKeyFromObject(&services[i])
		if !seen[key] {
			requests = append(requests, reconcile.Request{NamespacedName: key})
			seen[key] = true
		}
	}
	return requests
}

func (m *KVServicePostgresMapper) MapConfigMap(ctx context.Context, obj client.Object) []reconcile.Request {
	if _, ok := obj.(*corev1.ConfigMap); !ok {
		return nil
	}
	return m.mapDatabaseDependency(ctx, obj.GetNamespace(), "configmap/"+obj.GetName())
}

func (m *KVServicePostgresMapper) MapAdminSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	if _, ok := obj.(*corev1.Secret); !ok {
		return nil
	}
	return m.mapDatabaseDependency(ctx, obj.GetNamespace(), "secret/"+obj.GetName())
}

func (m *KVServicePostgresMapper) mapDatabaseDependency(ctx context.Context, namespace, dependency string) []reconcile.Request {
	var databases platformApi.PostgresDatabaseList
	if err := m.Client.List(ctx, &databases, client.InNamespace(namespace), client.MatchingFields{KVServiceDatabaseDependencyIndex: dependency}); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "could not map KVService database dependency", "namespace", namespace, "dependency", dependency, "error", err)
		return nil
	}
	var requests []reconcile.Request
	seen := make(map[client.ObjectKey]bool)
	for i := range databases.Items {
		for _, request := range databaseKVServiceRequests(&databases.Items[i]) {
			if !seen[request.NamespacedName] {
				requests = append(requests, request)
				seen[request.NamespacedName] = true
			}
		}
	}
	return requests
}

// MapTenantNamespace wakes all provisioners so removals, invalid metadata and
// backend changes can revoke previous membership. It never lists tenant namespaces.
func (m *KVServicePostgresMapper) MapTenantNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	if _, ok := obj.(*corev1.Namespace); !ok {
		return nil
	}
	services, err := ListKVServices(ctx, m.Client)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "could not list KVServices for tenant discovery", "error", err)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(services))
	for i := range services {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&services[i])})
	}
	return requests
}

// MapTenantNamespaceSecret resolves the one selected backend from its Namespace.
func (m *KVServicePostgresMapper) MapTenantNamespaceSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok || secret.Name != splcommon.GetNamespaceScopedSecretName(secret.Namespace) {
		return nil
	}
	namespace := &corev1.Namespace{}
	if err := m.Client.Get(ctx, client.ObjectKey{Name: secret.Namespace}, namespace); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "could not read tenant namespace for authentication Secret", "namespace", secret.Namespace, "error", err)
		return nil
	}
	if namespace.DeletionTimestamp != nil || namespace.Labels[splcommon.LabelKVService] != splcommon.LabelKVServiceEnabled {
		return nil
	}
	if _, err := splutil.GetTenantName(namespace); err != nil {
		return nil
	}
	endpoint, err := namespaceKVServiceAddress(ctx, namespace)
	if err != nil {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: endpoint.Namespace, Name: endpoint.KVServiceName}}}
}
