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
	"fmt"
	"log/slog"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/predicates"
	sharedreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ClusterTotalWorker int = 2

	// indexExternalSuperuserSecret maps a PostgresCluster CR by the external
	// Secret name it references via spec.passwordConfig.superuserExternalSecretRef.
	// Used by enqueueClustersForExternalSecret to find clusters that care about
	// a given Secret when that Secret is not owned by the cluster.
	indexExternalSuperuserSecret = "spec.passwordConfig.superuserExternalSecretRef.name"
)

// PostgresClusterReconciler reconciles PostgresCluster resources.
type PostgresClusterReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	Metrics        ports.Recorder
	FleetCollector *pgprometheus.FleetCollector
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusterclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := slog.Default().With("controller", "PostgresCluster", "name", req.Name, "namespace", req.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)
	rc := &clustercore.ReconcileContext{Client: r.Client, Scheme: r.Scheme, Recorder: r.Recorder, Metrics: r.Metrics}
	result, err := clustercore.PostgresClusterService(ctx, rc, req)
	r.FleetCollector.CollectClusterMetrics(ctx, r.Client, r.Metrics)
	if sharedreconcile.IsPureConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	return result, err
}

// SetupWithManager registers the controller, owned resource watches, and an
// external-secret watch that closes the observability gap for Secrets
// referenced via spec.passwordConfig but not owned by the PostgresCluster.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&enterprisev4.PostgresCluster{},
		indexExternalSuperuserSecret,
		extractExternalSuperuserSecretName,
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(predicate.Funcs{GenericFunc: func(event.GenericEvent) bool { return false }}).
		For(&enterprisev4.PostgresCluster{}, builder.WithPredicates(postgresClusterPredicator())).
		Owns(&cnpgv1.Cluster{}, builder.WithPredicates(cnpgClusterPredicator())).
		Owns(&cnpgv1.Pooler{}, builder.WithPredicates(cnpgPoolerPredicator())).
		Owns(&cnpgv1.ScheduledBackup{}, builder.WithPredicates(scheduledBackupPredicator())).
		Owns(&corev1.Secret{}, builder.WithPredicates(secretPredicator())).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(configMapPredicator())).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueClustersForExternalSecret),
			builder.WithPredicates(predicates.ExternalSecret())).
		Named("postgresCluster").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: ClusterTotalWorker,
		}).
		Complete(r)
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

// cnpgClusterPredicator triggers on spec changes, phase changes, scale progress,
// or primary changes. Generation catches spec drift
// before CNPG reflects it in status. Instance counts and CurrentPrimary are
// watched explicitly because CNPG keeps Phase=Healthy during scale-down; the
// only signal that anything is happening is ReadyInstances ticking down.
func cnpgClusterPredicator() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldObj, ok := e.ObjectOld.(*cnpgv1.Cluster)
				if !ok {
					slog.Error("predicate type assertion failed",
						"predicate", "cnpgClusterPredicator", "field", "ObjectOld",
						"got", fmt.Sprintf("%T", e.ObjectOld))
					return false
				}
				newObj, ok := e.ObjectNew.(*cnpgv1.Cluster)
				if !ok {
					slog.Error("predicate type assertion failed",
						"predicate", "cnpgClusterPredicator", "field", "ObjectNew",
						"got", fmt.Sprintf("%T", e.ObjectNew))
					return false
				}
				return oldObj.Status.Phase != newObj.Status.Phase ||
					oldObj.Status.Instances != newObj.Status.Instances ||
					oldObj.Status.ReadyInstances != newObj.Status.ReadyInstances ||
					oldObj.Status.CurrentPrimary != newObj.Status.CurrentPrimary
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
				oldObj, ok := e.ObjectOld.(*cnpgv1.Pooler)
				if !ok {
					slog.Error("predicate type assertion failed",
						"predicate", "cnpgPoolerPredicator", "field", "ObjectOld",
						"got", fmt.Sprintf("%T", e.ObjectOld))
					return false
				}
				newObj, ok := e.ObjectNew.(*cnpgv1.Pooler)
				if !ok {
					slog.Error("predicate type assertion failed",
						"predicate", "cnpgPoolerPredicator", "field", "ObjectNew",
						"got", fmt.Sprintf("%T", e.ObjectNew))
					return false
				}
				return oldObj.Status.Instances != newObj.Status.Instances
			},
		},
	)
}

// scheduledBackupPredicator triggers on spec changes or schedule time updates.
func scheduledBackupPredicator() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldObj, ok := e.ObjectOld.(*cnpgv1.ScheduledBackup)
				if !ok {
					slog.Error("predicate type assertion failed",
						"predicate", "scheduledBackupPredicator", "field", "ObjectOld",
						"got", fmt.Sprintf("%T", e.ObjectOld))
					return false
				}
				newObj, ok := e.ObjectNew.(*cnpgv1.ScheduledBackup)
				if !ok {
					slog.Error("predicate type assertion failed",
						"predicate", "scheduledBackupPredicator", "field", "ObjectNew",
						"got", fmt.Sprintf("%T", e.ObjectNew))
					return false
				}
				return !oldObj.Status.LastScheduleTime.Equal(newObj.Status.LastScheduleTime) ||
					!oldObj.Status.NextScheduleTime.Equal(newObj.Status.NextScheduleTime)
			},
			DeleteFunc: func(event.DeleteEvent) bool { return true },
		},
	)
}

// secretPredicator triggers only on deletion to recreate missing secrets.
// Updates are suppressed to avoid reconciling on content changes or ownership transitions.
func secretPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return false },
		UpdateFunc: func(event.UpdateEvent) bool { return false },
	}
}

// ConfigMap has no Generation, so ResourceVersionChangedPredicate would fire on label/annotation
// mutations (e.g. admission webhooks, kubectl annotate) that don't affect data content.
func configMapPredicator() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldCM := e.ObjectOld.(*corev1.ConfigMap)
			newCM := e.ObjectNew.(*corev1.ConfigMap)
			return !equality.Semantic.DeepEqual(oldCM.Data, newCM.Data)
		},
	}
}

// extractExternalSuperuserSecretName returns the external Secret name a
// PostgresCluster references via PasswordConfig, or nil when not in external
// mode. Used by the controller-runtime field indexer; called per CR on cache
// hydration and per CR update, so it must be cheap and total (no I/O).
//
// Package-private so unit tests can drive it directly without a fake client.
func extractExternalSuperuserSecretName(obj client.Object) []string {
	pc, ok := obj.(*enterprisev4.PostgresCluster)
	if !ok {
		return nil
	}
	if pc.Spec.PasswordConfig == nil {
		return nil
	}
	if name := pc.Spec.PasswordConfig.SuperuserExternalSecretRef.Name; name != "" {
		return []string{name}
	}
	return nil
}

// enqueueClustersForExternalSecret maps a Secret event to every PostgresCluster
// in the Secret's namespace whose spec.passwordConfig.superuserExternalSecretRef
// targets it. Owned Secrets are skipped because Owns(&corev1.Secret{}) already
// handles them — feeding both paths would queue duplicate reconciles.
//
// Runs on the event-source goroutine, so the index lookup is the only allowed
// work here; no blocking calls.
func (r *PostgresClusterReconciler) enqueueClustersForExternalSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	if owner := metav1.GetControllerOf(secret); owner != nil &&
		owner.APIVersion == enterprisev4.GroupVersion.String() &&
		owner.Kind == "PostgresCluster" {
		return nil
	}

	logger := logging.FromContext(ctx).With(
		"controller", "PostgresCluster",
		"func", "enqueueClustersForExternalSecret",
		"secret", secret.Name,
		"namespace", secret.Namespace,
	)

	var list enterprisev4.PostgresClusterList
	if err := r.Client.List(ctx, &list,
		client.InNamespace(secret.Namespace),
		client.MatchingFields{indexExternalSuperuserSecret: secret.Name},
	); err != nil {
		logger.ErrorContext(ctx, "failed to list PostgresClusters for external secret", "error", err)
		return nil
	}

	reqs := make([]reconcile.Request, 0, len(list.Items))
	for _, pc := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pc)})
		logger.InfoContext(ctx, "enqueuing PostgresCluster for external secret event",
			"postgresCluster", pc.Name)
	}
	return reqs
}
