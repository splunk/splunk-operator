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
	majorupgradeadapter "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/major_version_upgrade"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	majorversionupgradetypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	majorversionupgrade "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case"
	cnpgadapter "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	clusterk8s "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/k8s"
	dbadapter "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/predicates"
	sharedreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases,verbs=get;list;watch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := slog.Default().With("controller", "PostgresCluster", "name", req.Name, "namespace", req.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)
	rc := &clustercore.ReconcileContext{Client: r.Client, Scheme: r.Scheme, Recorder: r.Recorder, Metrics: r.Metrics, UseCaseRegistryProvider: r.useCaseRegistry}
	result, err := clustercore.PostgresClusterService(ctx, rc, req, dbadapter.NewRoleSweeper, cnpgadapter.NewBackupBackend(r.Client, r.Scheme))
	r.FleetCollector.CollectClusterMetrics(ctx, r.Client, r.Metrics)
	if sharedreconcile.IsPureConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	return result, err
}

// useCaseRegistry decides which use cases may run this reconcile pass and
// returns a dumb factory for each. Relevance is decided HERE, at registry-build
// time, where the live cluster spec is in hand: a use case whose feature is
// switched off is simply omitted from the map — never constructed, never
// scheduled — so the steady-state cost of an inactive feature is one cheap
// check here, not adapter construction or status reads. The factories
// themselves contain no decision logic; they only build. Adding a use case
// means adding one relevance check + factory here, not growing a shared ports
// aggregate built up front.
func (r *PostgresClusterReconciler) useCaseRegistry(key types.NamespacedName, cluster *enterprisev4.PostgresCluster, mergedConfig *clustercore.MergedConfig) map[string]usecases.Factory {
	return map[string]usecases.Factory{
		majorversionupgradetypes.UseCaseName: r.newMajorUpgradeUseCase(key, cluster, mergedConfig),
	}
}

// newMajorUpgradeUseCase is the dumb factory for the major-version upgrade use
// case: it closes over the per-reconcile runtime state and wires the three
// adapters when invoked, with no relevance logic of its own (useCaseRegistry
// already decided this use case is relevant before registering it).
func (r *PostgresClusterReconciler) newMajorUpgradeUseCase(key types.NamespacedName, cluster *enterprisev4.PostgresCluster, mergedConfig *clustercore.MergedConfig) usecases.Factory {
	return func() usecases.UseCase {
		targetVersion := ""
		if mergedConfig != nil && mergedConfig.Spec != nil && mergedConfig.Spec.PostgresVersion != nil {
			targetVersion = *mergedConfig.Spec.PostgresVersion
		}

		backupMethod, backupPluginName, providerConfigured := clustercore.EffectiveBackupProvider(mergedConfig)

		stateStore := clusterk8s.NewClusterStateStore(r.Client, key)
		infoStore := majorupgradeadapter.NewMajorUpgradeStateStoreWithTarget(stateStore, targetVersion)
		driver := majorupgradeadapter.NewPgUpgradeDriver(r.Client, key, targetVersion)

		var notifier *majorupgradeadapter.UpgradeNotifier
		if cluster != nil {
			notifier = majorupgradeadapter.NewUpgradeNotifier(&recorderEventEmitter{r.Recorder}, cluster)
		}
		if !providerConfigured {
			return majorversionupgrade.NewMajorUpgradeUseCase(infoStore, nil, driver, notifier)
		}
		rollback := majorupgradeadapter.NewRollbackCapabilityAdapter(r.Client, r.Scheme, key, backupMethod, backupPluginName)
		return majorversionupgrade.NewMajorUpgradeUseCase(infoStore, rollback, driver, notifier)
	}
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

	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(predicate.Funcs{GenericFunc: func(event.GenericEvent) bool { return false }}).
		For(&enterprisev4.PostgresCluster{}, builder.WithPredicates(postgresClusterPredicator())).
		Owns(&cnpgv1.Cluster{}, builder.WithPredicates(cnpgClusterPredicator())).
		Owns(&cnpgv1.Pooler{}, builder.WithPredicates(cnpgPoolerPredicator())).
		Owns(&cnpgv1.ScheduledBackup{}, builder.WithPredicates(scheduledBackupPredicator())).
		Owns(&corev1.Secret{}, builder.WithPredicates(secretPredicator())).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(configMapPredicator())).
		Watches(&enterprisev4.PostgresDatabase{},
			handler.EnqueueRequestsFromMapFunc(mapDatabaseToCluster),
			builder.WithPredicates(postgresDatabaseForClusterPredicator())).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueClustersForExternalSecret),
			builder.WithPredicates(predicates.ExternalSecret()))

	// The barman-cloud ObjectStore CRD is optional. Only register an owner watch when
	// the CRD is installed — Owns() on an unregistered GVK would fail informer startup.
	// When present, this re-enqueues the owning PostgresCluster on out-of-band edits or
	// deletion of the operator-managed ObjectStore so drift is repaired promptly.
	installed, err := objectStoreCRDInstalled(mgr)
	if err != nil {
		// A discovery error other than "CRD absent" (e.g. a transient failure during
		// startup) must not silently disable the watch for the controller's lifetime —
		// surface it so manager startup fails and is retried.
		return fmt.Errorf("probing barman-cloud ObjectStore CRD presence: %w", err)
	}
	if installed {
		objectStore := &unstructured.Unstructured{}
		objectStore.SetGroupVersionKind(clustercore.ObjectStoreGVK)
		ctrlBuilder = ctrlBuilder.Owns(objectStore, builder.WithPredicates(objectStorePredicator()))
	} else {
		slog.Info("barman-cloud ObjectStore CRD not installed; skipping ObjectStore owner watch",
			"controller", "postgresCluster")
	}

	return ctrlBuilder.
		Named("postgresCluster").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: ClusterTotalWorker,
		}).
		Complete(r)
}

// objectStoreCRDInstalled reports whether the barman-cloud ObjectStore CRD is registered
// in the cluster, via the manager's RESTMapper. A no-match (CRD absent) is reported as
// (false, nil) so the operator runs on clusters without the plugin; any other discovery
// error is returned so the caller can fail startup rather than permanently skip the watch.
func objectStoreCRDInstalled(mgr ctrl.Manager) (bool, error) {
	gvk := clustercore.ObjectStoreGVK
	_, err := mgr.GetRESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err == nil {
		return true, nil
	}
	if meta.IsNoMatchError(err) {
		return false, nil
	}
	return false, err
}

// objectStorePredicator re-enqueues on spec changes and deletion of an owned ObjectStore.
// Status-only updates and creates (the operator just made it) are ignored to avoid churn.
func objectStorePredicator() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
		},
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		CreateFunc: func(event.CreateEvent) bool { return false },
	}
}

func mapDatabaseToCluster(_ context.Context, obj client.Object) []reconcile.Request {
	db, ok := obj.(*enterprisev4.PostgresDatabase)
	if !ok || db.Spec.ClusterRef.Name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: db.Namespace, Name: db.Spec.ClusterRef.Name}}}
}

func postgresDatabaseForClusterPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			db, ok := e.Object.(*enterprisev4.PostgresDatabase)
			return ok && len(db.Status.Databases) > 0
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDB, oldOK := e.ObjectOld.(*enterprisev4.PostgresDatabase)
			newDB, newOK := e.ObjectNew.(*enterprisev4.PostgresDatabase)
			if !oldOK || !newOK {
				return false
			}
			if oldDB.Spec.ClusterRef.Name != newDB.Spec.ClusterRef.Name {
				return true
			}
			if !equality.Semantic.DeepEqual(oldDB.GetDeletionTimestamp(), newDB.GetDeletionTimestamp()) {
				return true
			}
			return !equality.Semantic.DeepEqual(oldDB.Status.Databases, newDB.Status.Databases)
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
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
				if retryAnnotationChanged(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) {
					return true
				}
				// Finalizer list change signals a cleanup lifecycle transition.
				return !equality.Semantic.DeepEqual(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers())
			},
		},
	)
}

func retryAnnotationChanged(oldAnnotations, newAnnotations map[string]string) bool {
	return oldAnnotations[majorversionupgradetypes.AnnotationMajorUpgradeRetryAt] !=
		newAnnotations[majorversionupgradetypes.AnnotationMajorUpgradeRetryAt]
}

// cnpgClusterPredicator triggers on spec changes, phase changes, scale progress,
// primary changes, or storage resize progress. Generation catches spec drift
// before CNPG reflects it in status. Instance counts and CurrentPrimary are
// watched explicitly because CNPG keeps Phase=Healthy during scale-down; the
// only signal that anything is happening is ReadyInstances ticking down.
// ResizingPVC is watched so the reconciler wakes when PVC expansion completes.
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
					oldObj.Status.CurrentPrimary != newObj.Status.CurrentPrimary ||
					len(oldObj.Status.ResizingPVC) != len(newObj.Status.ResizingPVC)
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

type recorderEventEmitter struct {
	recorder record.EventRecorder
}

func (e *recorderEventEmitter) EmitNormalEvent(obj client.Object, reason, message string) {
	e.recorder.Event(obj, corev1.EventTypeNormal, reason, message)
}

func (e *recorderEventEmitter) EmitWarningEvent(obj client.Object, reason, message string) {
	e.recorder.Event(obj, corev1.EventTypeWarning, reason, message)
}
