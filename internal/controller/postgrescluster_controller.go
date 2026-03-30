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
		WithEventFilter(predicate.Funcs{GenericFunc: func(event.GenericEvent) bool { return false }}).
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

func ownerReferencesChanged(oldObj, newObj metav1.Object) bool {
	return !equality.Semantic.DeepEqual(oldObj.GetOwnerReferences(), newObj.GetOwnerReferences())
}

// postgresClusterPredicator triggers on spec changes, deletion, and finalizer transitions.
func postgresClusterPredicator() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// DeletionTimestamp set means the object entered the deletion phase.
				if !equality.Semantic.DeepEqual(e.ObjectOld.GetDeletionTimestamp(), e.ObjectNew.GetDeletionTimestamp()) {
					return true
				}
				// Finalizer list change signals a cleanup lifecycle transition.
				return !equality.Semantic.DeepEqual(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers())
			},
		},
	)
}

// cnpgClusterPredicator triggers on spec changes, phase changes, or owner reference changes.
// Generation catches spec drift before CNPG reflects it in status.
func cnpgClusterPredicator() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldObj := e.ObjectOld.(*cnpgv1.Cluster)
				newObj := e.ObjectNew.(*cnpgv1.Cluster)
				return oldObj.Status.Phase != newObj.Status.Phase ||
					ownerReferencesChanged(oldObj, newObj)
			},
		},
	)
}

// cnpgPoolerPredicator triggers on spec changes or instance count changes.
// Generation catches spec drift before CNPG reflects it in instance status.
func cnpgPoolerPredicator() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldObj := e.ObjectOld.(*cnpgv1.Pooler)
				newObj := e.ObjectNew.(*cnpgv1.Pooler)
				return oldObj.Status.Instances != newObj.Status.Instances
			},
		},
	)
}

// secretPredicator triggers only when ownership changes.
// In retain-state mode we release ownership (remove ownerRef) without deleting the Secret,
// so this transition must trigger reconciliation to update our tracking state.
func secretPredicator() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return ownerReferencesChanged(e.ObjectOld, e.ObjectNew)
		},
	}
}

// configMapPredicator triggers on any content change.
// ConfigMap has no status subresource, so every resourceVersion bump is a real change.
func configMapPredicator() predicate.Predicate {
	return predicate.ResourceVersionChangedPredicate{}
}
