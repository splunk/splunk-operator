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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KVServicePostgresMapper maps PostgreSQL dependency events to their KVService consumers.
type KVServicePostgresMapper struct {
	Client client.Client
}

func (m *KVServicePostgresMapper) mapConsumers(ctx context.Context, namespace string, matches func(*enterpriseApi.KVService) bool) []reconcile.Request {
	var list enterpriseApi.KVServiceList
	if err := m.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "could not list KVServices for PostgreSQL dependency", "namespace", namespace, "error", err)
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		cr := &list.Items[i]
		if cr.Spec.Postgres != nil && matches(cr) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cr)})
		}
	}
	return requests
}

// MapDatabase enqueues KVServices referencing the changed PostgresDatabase.
func (m *KVServicePostgresMapper) MapDatabase(ctx context.Context, obj client.Object) []reconcile.Request {
	database, ok := obj.(*platformApi.PostgresDatabase)
	if !ok {
		return nil
	}
	return m.mapConsumers(ctx, database.Namespace, func(cr *enterpriseApi.KVService) bool {
		return KVServicePostgresDatabaseResourceName(cr) == database.Name
	})
}

// MapConfigMap enqueues consumers of connection settings published in database status.
func (m *KVServicePostgresMapper) MapConfigMap(ctx context.Context, obj client.Object) []reconcile.Request {
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	return m.mapDatabaseDependency(ctx, configMap.Namespace, func(info platformApi.DatabaseInfo) bool {
		return info.ConfigMapRef != nil && info.ConfigMapRef.Name == configMap.Name
	})
}

// MapAdminSecret enqueues consumers of credentials published in database status.
func (m *KVServicePostgresMapper) MapAdminSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}
	return m.mapDatabaseDependency(ctx, secret.Namespace, func(info platformApi.DatabaseInfo) bool {
		return info.AdminUserSecretRef != nil && info.AdminUserSecretRef.Name == secret.Name
	})
}

// MapNamespaceSecret enqueues consumers of the namespace authentication Secret.
func (m *KVServicePostgresMapper) MapNamespaceSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok || secret.Name != splcommon.GetNamespaceScopedSecretName(secret.Namespace) {
		return nil
	}
	return m.mapConsumers(ctx, secret.Namespace, func(*enterpriseApi.KVService) bool { return true })
}

func (m *KVServicePostgresMapper) mapDatabaseDependency(ctx context.Context, namespace string, matches func(platformApi.DatabaseInfo) bool) []reconcile.Request {
	return m.mapConsumers(ctx, namespace, func(cr *enterpriseApi.KVService) bool {
		key := types.NamespacedName{Namespace: namespace, Name: KVServicePostgresDatabaseResourceName(cr)}
		database := &platformApi.PostgresDatabase{}
		if err := m.Client.Get(ctx, key, database); err != nil {
			logger := logging.FromContext(ctx)
			if apierrors.IsNotFound(err) {
				logger.DebugContext(ctx, "PostgresDatabase dependency is not available yet", "database", key, "kvservice", cr.Name)
			} else {
				logger.ErrorContext(ctx, "could not get PostgresDatabase for KVService dependency mapping", "database", key, "kvservice", cr.Name, "error", err)
			}
			return false
		}
		for _, info := range database.Status.Databases {
			if info.Name == KVServicePostgresLogicalDatabaseName(cr) && matches(info) {
				return true
			}
		}
		return false
	})
}
