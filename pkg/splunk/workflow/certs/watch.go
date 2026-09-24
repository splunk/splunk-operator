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

package certs

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
)

// CertSecretMapper returns a handler.MapFunc that maps a cert Secret update to
// the owning CR reconcile requests. It lists all CRs of the given type in the
// secret's namespace and enqueues those that reference the secret in their
// spec.certs[] or via CertificateRequester.Certificates().
//
// Usage in a controller SetupWithManager:
//
//	Watches(&corev1.Secret{},
//	    handler.EnqueueRequestsFromMapFunc(
//	        certs.CertSecretMapper(mgr.GetClient(), &enterpriseApi.StandaloneList{})))
func CertSecretMapper(c client.Client, crList client.ObjectList) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		if err := c.List(ctx, crList, client.InNamespace(secret.Namespace)); err != nil {
			logging.FromContext(ctx).With("func", "CertSecretMapper").ErrorContext(ctx, "failed to list CRs",
				"secret", secret.Name, "namespace", secret.Namespace, "error", err)
			return nil
		}

		var requests []reconcile.Request
		items := crListItems(crList)
		for _, cr := range items {
			if certSecretReferencedBy(secret.Name, cr) {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: cr.GetNamespace(),
						Name:      cr.GetName(),
					},
				})
			}
		}
		return requests
	}
}

// certSecretReferencedBy returns true if the CR references secretName in
// spec.certs[] or via CertificateRequester.Certificates().
func certSecretReferencedBy(secretName string, cr client.Object) bool {
	// Check CertificateRequester (operator-driven).
	if requester, ok := cr.(CertificateRequester); ok {
		for _, s := range requester.Certificates() {
			if s == secretName {
				return true
			}
		}
	}
	// Check spec.certs[] (user-declared) via GetCerts interface.
	// Only v4 CR types implement this — v3 types (ClusterMaster, LicenseMaster)
	// are deprecated and intentionally do not receive new API fields.
	// The consequence is that cert rotation is not triggered for user-declared
	// spec.certs[] on v3 CRs; operator-driven CertificateRequester.Certificates()
	// above still works for v3.
	type certsGetter interface {
		GetCerts() []enterpriseApi.CertSpec
	}
	if g, ok := cr.(certsGetter); ok {
		for _, cs := range g.GetCerts() {
			if cs.SecretRef.Name == secretName {
				return true
			}
		}
	}
	return false
}

// crListItems extracts the individual items from any typed CR list.
// Supports all 9 SOK enterprise CR list types.
func crListItems(list client.ObjectList) []client.Object {
	switch l := list.(type) {
	case *enterpriseApi.StandaloneList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApi.IndexerClusterList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApi.SearchHeadClusterList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApi.ClusterManagerList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApi.LicenseManagerList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApi.MonitoringConsoleList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApi.IngestorClusterList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApiV3.ClusterMasterList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *enterpriseApiV3.LicenseMasterList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	}
	return nil
}
