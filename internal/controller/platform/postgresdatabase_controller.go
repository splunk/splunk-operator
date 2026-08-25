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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbadapter "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter"
	dbmetricsadapter "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter/custom_metrics"
	dbcore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	dbmetrics "github.com/splunk/splunk-operator/pkg/postgresql/database/core/custom_metrics"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/predicates"
	sharedreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"

	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/splunk/splunk-operator/pkg/logging"
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

// PostgresDatabaseReconciler reconciles a PostgresDatabase object.
type PostgresDatabaseReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	Metrics        ports.Recorder
	FleetCollector *pgprometheus.FleetCollector
}

const (
	DatabaseTotalWorker int = 2

	// indexExternalRoleSecrets maps a PostgresDatabase CR by every external
	// admin/RW Secret name it references via spec.databases[*].passwordConfig.
	// A single key covers both admin and rw refs because Secret events arrive
	// by-name and we only need to know "which PostgresDatabase cares about
	// this Secret", not which role-side it sits on.
	indexExternalRoleSecrets = "spec.databases.passwordConfig.externalSecretRefs"
)

//+kubebuilder:rbac:groups=platform.splunk.com,resources=postgresdatabases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=platform.splunk.com,resources=postgresdatabases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=platform.splunk.com,resources=postgresdatabases/finalizers,verbs=update
//+kubebuilder:rbac:groups=platform.splunk.com,resources=postgresclusters,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *PostgresDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := slog.Default().With("controller", "PostgresDatabase", "name", req.Name, "namespace", req.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	postgresDB := &platformv1alpha1.PostgresDatabase{}
	if err := r.Get(ctx, req.NamespacedName, postgresDB); err != nil {
		if apierrors.IsNotFound(err) {
			logger.InfoContext(ctx, "PostgresDatabase resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	rc := &dbcore.ReconcileContext{
		Client:   r.Client,
		Scheme:   r.Scheme,
		Recorder: r.Recorder,
		Metrics:  r.Metrics,
		NewCustomMetricsAcknowledgementRepo: func(cluster *platformv1alpha1.PostgresCluster) dbmetrics.AcknowledgementRepository {
			return dbmetricsadapter.NewAcknowledgementRepository(cluster.Status.CustomMetricsStatus)
		},
	}
	result, err := dbcore.PostgresDatabaseService(ctx, rc, postgresDB, dbadapter.NewDBRepository)
	r.FleetCollector.CollectDatabaseMetrics(ctx, r.Client, r.Metrics)
	if sharedreconcile.IsPureConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&cnpgv1.Database{},
		".metadata.controller",
		func(obj client.Object) []string {
			owner := metav1.GetControllerOf(obj)
			if owner == nil {
				return nil
			}
			if owner.APIVersion != platformv1alpha1.GroupVersion.String() || owner.Kind != "PostgresDatabase" {
				return nil
			}
			return []string{owner.Name}
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&platformv1alpha1.PostgresDatabase{},
		indexExternalRoleSecrets,
		extractExternalRoleSecretNames,
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&platformv1alpha1.PostgresDatabase{},
		platformv1alpha1.PostgresDatabaseClusterRefNameField,
		extractPostgresDatabaseClusterRefName,
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(predicate.Funcs{GenericFunc: func(event.GenericEvent) bool { return false }}).
		For(&platformv1alpha1.PostgresDatabase{}, builder.WithPredicates(postgresDatabasePredicator())).
		Owns(&cnpgv1.Database{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Secret{}, builder.WithPredicates(databaseSecretPredicator())).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&platformv1alpha1.PostgresCluster{},
			handler.EnqueueRequestsFromMapFunc(r.enqueuePostgresDatabasesForCluster),
			builder.WithPredicates(postgresClusterForDatabasePredicator())).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.enqueuePostgresDatabasesForExternalSecret),
			builder.WithPredicates(predicates.ExternalSecret())).
		Named("postgresdatabase").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: DatabaseTotalWorker,
		}).
		Complete(r)
}

// databaseSecretPredicator triggers only on deletion to recreate missing role secrets.
// Updates are suppressed — reconcileRoleSecrets only touches ownership, never secret data.
func databaseSecretPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return false },
		UpdateFunc: func(event.UpdateEvent) bool { return false },
	}
}

func postgresDatabasePredicator() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				if !equality.Semantic.DeepEqual(e.ObjectOld.GetDeletionTimestamp(), e.ObjectNew.GetDeletionTimestamp()) {
					return true
				}
				return !equality.Semantic.DeepEqual(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers())
			},
		},
	)
}

func roUnavailable(readyInstances *int32) bool {
	return readyInstances == nil || *readyInstances < 2
}

// Watches cluster status consumed by database gates; replica changes matter only
// when they cross the read-only availability threshold.
func postgresClusterForDatabasePredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldCluster, oldOK := e.ObjectOld.(*platformv1alpha1.PostgresCluster)
			newCluster, newOK := e.ObjectNew.(*platformv1alpha1.PostgresCluster)
			if !oldOK || !newOK {
				return false
			}
			if !equality.Semantic.DeepEqual(oldCluster.Status.ConnectionPoolerStatus, newCluster.Status.ConnectionPoolerStatus) {
				return true
			}
			if !equality.Semantic.DeepEqual(oldCluster.Status.ManagedRolesStatus, newCluster.Status.ManagedRolesStatus) {
				return true
			}
			if !equality.Semantic.DeepEqual(oldCluster.Status.CustomMetricsStatus, newCluster.Status.CustomMetricsStatus) {
				return true
			}
			return roUnavailable(oldCluster.Status.ReadyInstances) != roUnavailable(newCluster.Status.ReadyInstances)
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func (r *PostgresDatabaseReconciler) enqueuePostgresDatabasesForCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	cluster, ok := obj.(*platformv1alpha1.PostgresCluster)
	if !ok {
		return nil
	}
	logger := logging.FromContext(ctx).With(
		"controller", "PostgresDatabase",
		"func", "enqueuePostgresDatabasesForCluster",
		"postgresCluster", cluster.Name,
		"namespace", cluster.Namespace,
	)

	var list platformv1alpha1.PostgresDatabaseList
	if err := r.Client.List(ctx, &list,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{platformv1alpha1.PostgresDatabaseClusterRefNameField: cluster.Name},
	); err != nil {
		logger.WarnContext(ctx, "indexed PostgresDatabase list failed, falling back to namespace list", "error", err)
		if fallbackErr := r.Client.List(ctx, &list, client.InNamespace(cluster.Namespace)); fallbackErr != nil {
			logger.ErrorContext(ctx, "failed to list PostgresDatabases for cluster", "error", fallbackErr)
			return nil
		}
	}

	reqs := make([]reconcile.Request, 0, len(list.Items))
	for _, db := range list.Items {
		if db.Spec.ClusterRef.Name != cluster.Name {
			continue
		}
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&db)})
		logger.InfoContext(ctx, "enqueuing PostgresDatabase for cluster recreation", "postgresDatabase", db.Name)
	}

	return reqs
}

func extractPostgresDatabaseClusterRefName(obj client.Object) []string {
	pd, ok := obj.(*platformv1alpha1.PostgresDatabase)
	if !ok || pd.Spec.ClusterRef.Name == "" {
		return nil
	}
	return []string{pd.Spec.ClusterRef.Name}
}

// extractExternalRoleSecretNames returns the de-duplicated set of external
// admin + RW Secret names referenced by any DatabaseDefinition in spec that
// has PasswordConfig set. Each unique name appears once so the index stays
// compact even when admin and RW refs collide or are reused across DBs.
//
// Package-private so unit tests can drive it directly without a fake client.
func extractExternalRoleSecretNames(obj client.Object) []string {
	pd, ok := obj.(*platformv1alpha1.PostgresDatabase)
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, len(pd.Spec.Databases)*2)
	var names []string
	for _, db := range pd.Spec.Databases {
		if db.PasswordConfig == nil {
			continue
		}
		for _, name := range [...]string{
			db.PasswordConfig.ExternalAdminSecretRef.Name,
			db.PasswordConfig.ExternalRWSecretRef.Name,
		} {
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

// enqueuePostgresDatabasesForExternalSecret maps a Secret event to every
// PostgresDatabase in the Secret's namespace whose
// spec.databases[*].passwordConfig references it by name. Owned Secrets are
// skipped because Owns(&corev1.Secret{}) already handles them.
//
// Runs on the event-source goroutine, so the index lookup is the only
// permitted work here; no blocking calls.
func (r *PostgresDatabaseReconciler) enqueuePostgresDatabasesForExternalSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	if owner := metav1.GetControllerOf(secret); owner != nil &&
		owner.APIVersion == platformv1alpha1.GroupVersion.String() &&
		owner.Kind == "PostgresDatabase" {
		return nil
	}

	logger := logging.FromContext(ctx).With(
		"controller", "PostgresDatabase",
		"func", "enqueuePostgresDatabasesForExternalSecret",
		"secret", secret.Name,
		"namespace", secret.Namespace,
	)

	var list platformv1alpha1.PostgresDatabaseList
	if err := r.Client.List(ctx, &list,
		client.InNamespace(secret.Namespace),
		client.MatchingFields{indexExternalRoleSecrets: secret.Name},
	); err != nil {
		logger.ErrorContext(ctx, "failed to list PostgresDatabases for external secret", "error", err)
		return nil
	}

	reqs := make([]reconcile.Request, 0, len(list.Items))
	for _, pd := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pd)})
		logger.InfoContext(ctx, "enqueuing PostgresDatabase for external secret event",
			"postgresDatabase", pd.Name)
	}
	return reqs
}
