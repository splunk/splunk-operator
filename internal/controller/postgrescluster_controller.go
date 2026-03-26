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

package controller

import (
	"context"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	ClusterTotalWorker int = 2
)

// PostgresClusterReconciler reconciles PostgresCluster resources.
type PostgresClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusterclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/status,verbs=get

func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return clustercore.PostgresClusterService(ctx, r.Client, r.Scheme, req)
}

// SetupWithManager registers the controller and owned resource watches.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresCluster{}, builder.WithPredicates(postgresClusterPredicator())).
		Owns(&cnpgv1.Cluster{}, builder.WithPredicates(cnpgClusterPredicator())).
		Owns(&cnpgv1.Pooler{}, builder.WithPredicates(cnpgPoolerPredicator())).
		Owns(&corev1.Secret{}, builder.WithPredicates(secretPredicator())).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(configMapPredicator())).
		Named("postgresCluster").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: ClusterTotalWorker,
		}).
		Complete(r)
}

func deletionTimestampChanged(oldObj, newObj metav1.Object) bool {
	return !equality.Semantic.DeepEqual(oldObj.GetDeletionTimestamp(), newObj.GetDeletionTimestamp())
}

func ownerReferencesChanged(oldObj, newObj metav1.Object) bool {
	return !equality.Semantic.DeepEqual(oldObj.GetOwnerReferences(), newObj.GetOwnerReferences())
}

// postgresClusterPredicator triggers on generation changes, deletion, and finalizer transitions.
func postgresClusterPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldOK := e.ObjectOld.(*enterprisev4.PostgresCluster)
			newObj, newOK := e.ObjectNew.(*enterprisev4.PostgresCluster)
			if !oldOK || !newOK {
				return true
			}
			if oldObj.Generation != newObj.Generation {
				return true
			}
			if deletionTimestampChanged(oldObj, newObj) {
				return true
			}
			// Finalizer changes indicate registration or deletion  always reconcile.
			return controllerutil.ContainsFinalizer(oldObj, clustercore.PostgresClusterFinalizerName) !=
				controllerutil.ContainsFinalizer(newObj, clustercore.PostgresClusterFinalizerName)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// cnpgClusterPredicator triggers only on phase changes or owner reference changes.
func cnpgClusterPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldOK := e.ObjectOld.(*cnpgv1.Cluster)
			newObj, newOK := e.ObjectNew.(*cnpgv1.Cluster)
			if !oldOK || !newOK {
				return true
			}
			return oldObj.Status.Phase != newObj.Status.Phase ||
				ownerReferencesChanged(oldObj, newObj)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// cnpgPoolerPredicator triggers only on instance count changes.
func cnpgPoolerPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldOK := e.ObjectOld.(*cnpgv1.Pooler)
			newObj, newOK := e.ObjectNew.(*cnpgv1.Pooler)
			if !oldOK || !newOK {
				return true
			}
			return oldObj.Status.Instances != newObj.Status.Instances
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// secretPredicator triggers only on owner reference changes.

// secretPredicator filters Secret events to trigger reconciles on creation, deletion, or owner reference changes.
func secretPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldOK := e.ObjectOld.(*corev1.Secret)
			newObj, newOK := e.ObjectNew.(*corev1.Secret)
			if !oldOK || !newOK {
				return true
			}
			return ownerReferencesChanged(oldObj, newObj)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// configMapPredicator triggers on data, label, annotation, or owner reference changes.
func configMapPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldOK := e.ObjectOld.(*corev1.ConfigMap)
			newObj, newOK := e.ObjectNew.(*corev1.ConfigMap)
			if !oldOK || !newOK {
				return true
			}
			return !equality.Semantic.DeepEqual(oldObj.Data, newObj.Data) ||
				!equality.Semantic.DeepEqual(oldObj.Labels, newObj.Labels) ||
				!equality.Semantic.DeepEqual(oldObj.Annotations, newObj.Annotations) ||
				ownerReferencesChanged(oldObj, newObj)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}
