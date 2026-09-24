/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package predicates holds controller-runtime event predicates shared across the
// PostgreSQL controllers.
package predicates

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ExternalSecret filters Secret events down to those carrying signal for an
// externally managed secret contract: Create, Delete, or any update touching
// .data or .metadata.labels. It suppresses pure resourceVersion churn so noisy
// namespaces don't storm the controller.
//
// The filter is resource-agnostic — what differs per controller is the mapping
// function that resolves which CRs reference the Secret — so both the
// PostgresCluster (superuser secret) and PostgresDatabase (admin/RW role
// secrets) watches share it. Owned-secret signals continue to flow through each
// controller's Owns(&corev1.Secret{}) chain on an independent watch.
func ExternalSecret() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldS, ok1 := e.ObjectOld.(*corev1.Secret)
			newS, ok2 := e.ObjectNew.(*corev1.Secret)
			if !ok1 || !ok2 {
				return false
			}
			return !equality.Semantic.DeepEqual(oldS.Data, newS.Data) ||
				!equality.Semantic.DeepEqual(oldS.Labels, newS.Labels)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}
