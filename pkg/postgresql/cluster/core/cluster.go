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

package core

import (
	"context"
	"errors"
	"fmt"

	"log/slog"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// PostgresClusterService is the application service entry point called by the primary adapter (reconciler).
func PostgresClusterService(ctx context.Context, rc *ReconcileContext, req ctrl.Request, newRoleSweeper ports.NewRoleSweeperFunc, backupBackend BackupBackend, recoveryBackend RecoveryBackend) (ctrl.Result, error) {
	c := rc.Client
	logger := logging.FromContext(ctx).With("func", "PostgresClusterService")
	logger.DebugContext(ctx, "reconciling PostgresCluster")

	var postgresSecretName string

	// 1. Fetch the PostgresCluster instance, stop if not found.
	postgresCluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, req.NamespacedName, postgresCluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.InfoContext(ctx, "PostgresCluster deleted, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch PostgresCluster: %w", err)
	}
	if postgresCluster.Status.Resources == nil {
		postgresCluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}

	logger = logger.With("postgresCluster", postgresCluster.Name)
	ctx = logging.WithLogger(ctx, logger)

	currentPhase := func() string {
		if postgresCluster.Status.Phase == nil {
			return ""
		}
		return *postgresCluster.Status.Phase
	}

	updateStatus := func(conditionType conditionTypes, status metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileClusterPhases) error {
		oldPhase := currentPhase()
		if err := setStatus(ctx, c, rc.Metrics, postgresCluster, postgresCluster.Status.DeepCopy(), conditionType, status, reason, message, phase); err != nil {
			return err
		}
		rc.emitClusterPhaseTransition(postgresCluster, oldPhase, currentPhase(), reason, message)
		return nil
	}
	updateComponentHealthStatus := func(before *enterprisev4.PostgresClusterStatus, health componentHealth) error {
		oldPhase := currentPhase()
		if err := setStatusFromHealth(ctx, c, rc.Metrics, postgresCluster, before, health); err != nil {
			return err
		}
		rc.emitClusterPhaseTransition(postgresCluster, oldPhase, currentPhase(), health.Reason, health.Message)
		return nil
	}
	updatePhaseStatus := func(phase reconcileClusterPhases) error {
		oldPhase := currentPhase()
		if err := setPhaseStatus(ctx, c, postgresCluster, phase); err != nil {
			return err
		}
		rc.emitClusterPhaseTransition(postgresCluster, oldPhase, currentPhase(), "", "")
		return nil
	}

	// Finalizer handling must come before any other processing.
	if err := handleFinalizer(ctx, rc, postgresCluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.InfoContext(ctx, "PostgresCluster already deleted, skipping finalizer update")
			return ctrl.Result{}, nil
		}
		rc.emitWarning(postgresCluster, EventCleanupFailed, fmt.Sprintf("cleanup failed for PostgresCluster %s — check operator logs", postgresCluster.Name))
		statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterDeleteFailed,
			fmt.Sprintf("Failed to delete resources during cleanup: %v", err), failedClusterPhase)
		return ctrl.Result{}, errors.Join(fmt.Errorf("failed to handle finalizer: %w", err), statusErr)
	}
	if postgresCluster.GetDeletionTimestamp() != nil {
		logger.InfoContext(ctx, "deletion cleanup complete, finalizer removed")
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(postgresCluster, PostgresClusterFinalizerName) {
		controllerutil.AddFinalizer(postgresCluster, PostgresClusterFinalizerName)
		if err := c.Update(ctx, postgresCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		logger.InfoContext(ctx, "finalizer added")
		return ctrl.Result{}, nil
	}

	// Load the referenced PostgresClusterClass.
	clusterClass := &enterprisev4.PostgresClusterClass{}
	if err := c.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, clusterClass); err != nil {
		rc.emitWarning(postgresCluster, EventClusterClassNotFound, fmt.Sprintf("ClusterClass %s not found for PostgresCluster %s", postgresCluster.Spec.Class, postgresCluster.Name))
		statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterClassNotFound,
			fmt.Sprintf("ClusterClass %s not found: %v", postgresCluster.Spec.Class, err), failedClusterPhase)
		return ctrl.Result{}, errors.Join(fmt.Errorf("failed to fetch PostgresClusterClass %s: %w", postgresCluster.Spec.Class, err), statusErr)
	}

	// Merge PostgresClusterSpec on top of PostgresClusterClass defaults.
	mergedConfig := GetMergedConfig(clusterClass, postgresCluster)
	configErrs := append(ValidateMergedConfig(mergedConfig, clusterClass.Name), ValidateCrossResource(clusterClass, postgresCluster)...)
	configErrs = append(configErrs, ValidateRecoveryCapabilities(recoveryBackend, clusterClass, postgresCluster)...)
	if len(configErrs) > 0 {
		var errMsgs []error
		for _, e := range configErrs {
			errMsgs = append(errMsgs, e)
		}
		err := errors.Join(errMsgs...)
		rc.emitWarning(postgresCluster, EventConfigMergeFailed, fmt.Sprintf("invalid configuration for PostgresCluster %s — check operator logs", postgresCluster.Name))
		statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonInvalidConfiguration,
			fmt.Sprintf("Failed to merge configuration: %v", err), failedClusterPhase)
		return ctrl.Result{}, errors.Join(fmt.Errorf("failed to merge PostgresCluster configuration: %w", err), statusErr)
	}

	// Resolve or derive the superuser secret name. An external secret reference
	// from the spec wins: the operator validates and tracks that secret instead
	// of creating its own (see secretModel.reconcileExternalSecret).
	switch {
	case postgresCluster.Spec.PasswordConfig != nil && postgresCluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name != "":
		postgresSecretName = postgresCluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name
		logger.InfoContext(ctx, "superuser secret resolved from spec", "name", postgresSecretName)
	case postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.SuperUserSecretRef != nil:
		postgresSecretName = postgresCluster.Status.Resources.SuperUserSecretRef.Name
		logger.InfoContext(ctx, "superuser secret resolved from status", "name", postgresSecretName)
	default:
		postgresSecretName = fmt.Sprintf("%s%s", postgresCluster.Name, defaultSecretSuffix)
		logger.InfoContext(ctx, "superuser secret name derived", "name", postgresSecretName)
	}

	contracts := &reconcileContracts{}
	components := []component{
		newSecretModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, postgresSecretName, contracts),
		newObjectStoreModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, mergedConfig),
		newClusterModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, clusterClass, mergedConfig, contracts),
		newManagedRolesModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, contracts, newRoleSweeper),
		newPoolerModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, clusterClass, mergedConfig, contracts),
		newBackupModel(backupBackend, rc, updateComponentHealthStatus, postgresCluster, mergedConfig, contracts),
		newConfigMapModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, contracts),
	}
	if err := validateComponentOrder(components); err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid component wiring: %w", err)
	}

	useCaseReconciler := newUseCaseReconciler(rc, req.NamespacedName, postgresCluster, mergedConfig)
	if useCaseReconciler != nil {
		// Prerequisites are checked inside Schedule: a use case whose prereqs
		// are unmet is silently deferred — it does not block components and
		// does not Act this pass.
		if err := useCaseReconciler.Schedule(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}

	result, err := runComponents(ctx, logger, components, blockedComponents(useCaseReconciler))
	if err != nil {
		return result, err
	}
	if result != (ctrl.Result{}) {
		return result, nil
	}

	useCaseReport, useCaseErr := reconcileUseCases(ctx, useCaseReconciler)
	if useCaseReport != nil || useCaseErr != nil {
		if useCaseErr != nil {
			logger.ErrorContext(ctx, "use case reconciliation failed",
				"error", useCaseErr,
				"name", useCaseReport.Name,
				"reason", useCaseReport.Reason,
				"phase", useCaseReport.Phase)
		} else {
			logger.InfoContext(ctx, "use case reconciled",
				"name", useCaseReport.Name,
				"reason", useCaseReport.Reason,
				"phase", useCaseReport.Phase)
		}
		return resultFromUseCaseReport(useCaseReport), useCaseErr
	}

	logger.DebugContext(ctx, "reconciliation complete")
	if err := updatePhaseStatus(readyClusterPhase); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func newUseCaseReconciler(rc *ReconcileContext, key types.NamespacedName, cluster *enterprisev4.PostgresCluster, mergedConfig *MergedConfig) *usecases.Reconciler {
	if rc == nil || rc.UseCaseRegistryProvider == nil {
		return nil
	}
	var spec *enterprisev4.PostgresClusterSpec
	if cluster != nil {
		spec = &cluster.Spec
	}
	return usecases.NewUseCaseReconciler(spec, rc.UseCaseRegistryProvider(key, cluster, mergedConfig))
}

func blockedComponents(reconciler *usecases.Reconciler) map[string]struct{} {
	if reconciler == nil {
		return nil
	}
	return reconciler.BlocksComponents()
}

func reconcileUseCases(ctx context.Context, reconciler *usecases.Reconciler) (*reconciliationTypes.Report, error) {
	if reconciler == nil {
		return nil, nil
	}
	return reconciler.Reconcile(ctx)
}

func resultFromUseCaseReport(report *reconciliationTypes.Report) ctrl.Result {
	if report == nil || !report.Retry {
		return ctrl.Result{}
	}
	if report.Sleep != nil && *report.Sleep > 0 {
		return ctrl.Result{RequeueAfter: time.Duration(*report.Sleep) * time.Second}
	}
	return ctrl.Result{Requeue: true}
}

func writeComponentStatus(updateStatus healthStatusUpdater, before *enterprisev4.PostgresClusterStatus, health componentHealth) error {
	if updateStatus == nil {
		return nil
	}
	return updateStatus(before, health)
}

func runComponents(ctx context.Context, logger *slog.Logger, components []component, blockedComponents ...map[string]struct{}) (ctrl.Result, error) {
	var blocked map[string]struct{}
	if len(blockedComponents) > 0 {
		blocked = blockedComponents[0]
	}
	for _, c := range components {
		componentLogger := logger.With("component", c.Name())
		if _, ok := blocked[c.Name()]; ok {
			componentLogger.InfoContext(ctx, "component reconciliation blocked by active use case")
			continue
		}
		var reconcileErr error
		if reconcileErr = c.CheckContracts(); reconcileErr == nil {
			reconcileErr = c.Reconcile(ctx)
		}
		health, err := c.Observe(ctx, reconcileErr)
		if err != nil {
			componentLogger.ErrorContext(ctx, "component observe failed",
				"error", err,
				"step", "observe",
				"condition", health.Condition,
				"reason", health.Reason,
				"phase", health.Phase)
			return health.Result, fmt.Errorf("%s observe: %w", c.Name(), err)
		}
		if isIntermediateState(health.State) {
			componentLogger.InfoContext(ctx, "component observe pending",
				"step", "observe",
				"condition", health.Condition,
				"reason", health.Reason,
				"phase", health.Phase,
				"requeueAfter", health.Result.RequeueAfter)
			return health.Result, nil
		}
		componentLogger.InfoContext(ctx, "component observe ready",
			"step", "observe",
			"condition", health.Condition,
			"reason", health.Reason,
			"phase", health.Phase)
	}
	return ctrl.Result{}, nil
}

// types/dto candidate
type componentHealth struct {
	State     pgcConstants.State
	Condition conditionTypes
	Reason    conditionReasons
	Message   string
	Phase     reconcileClusterPhases
	Result    ctrl.Result
}

// newReadyHealth marks a component as fully reconciled — its desired state matches
// the actual state and no further action is needed this cycle.
// Phase is intentionally left empty: individual components must not set the cluster
// phase to Ready — that is the responsibility of the top-level reconciler once all
// components have converged (via updatePhaseStatus at the end of Reconcile).
func newReadyHealth(cond conditionTypes, reason conditionReasons, msg string) componentHealth {
	return componentHealth{Condition: cond, State: pgcConstants.Ready, Reason: reason, Message: msg}
}

// newFailedHealth marks a component that hit a terminal error it cannot recover from
// on its own — operator intervention or a spec change is required.
func newFailedHealth(cond conditionTypes, reason conditionReasons, msg string) componentHealth {
	return componentHealth{Condition: cond, State: pgcConstants.Failed, Reason: reason, Message: msg, Phase: failedClusterPhase}
}

// newPendingHealth marks a component that is blocked waiting for an upstream object
// to be created. The cluster stays at Pending and is requeued until the dependency appears.
func newPendingHealth(cond conditionTypes, reason conditionReasons, msg string) componentHealth {
	return componentHealth{Condition: cond, State: pgcConstants.Pending, Reason: reason, Message: msg, Phase: pendingClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
}

// newProvisioningHealth marks a component whose upstream object exists but has not
// reached its desired state yet — e.g. a CNPG cluster that is still initialising replicas.
func newProvisioningHealth(cond conditionTypes, reason conditionReasons, msg string) componentHealth {
	return componentHealth{Condition: cond, State: pgcConstants.Provisioning, Reason: reason, Message: msg, Phase: provisioningClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
}

// newConfiguringHealth marks a component whose upstream resource is healthy but is
// actively applying a change — e.g. a switchover, rolling restart, or config rollout.
// The cluster is operational but not yet settled; requeued until the change completes.
func newConfiguringHealth(cond conditionTypes, reason conditionReasons, msg string) componentHealth {
	return componentHealth{Condition: cond, State: pgcConstants.Configuring, Reason: reason, Message: msg, Phase: configuringClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
}

type component interface {
	Reconcile(ctx context.Context) error
	Observe(ctx context.Context, reconcileErr error) (componentHealth, error)
	CheckContracts() error
	Name() string
	Requires() []contractKey
	Provides() []contractKey
}

type healthStatusUpdater func(before *enterprisev4.PostgresClusterStatus, health componentHealth) error

// classifyReconcileErr inspects reconcileErr and returns the appropriate componentHealth
// and error for the two sentinel cases every Observe method must handle before doing its
// own observation work:
//
//   - errContractsNotReady — an upstream component has not yet published its contract
//     object (e.g. the CNPG Cluster or Secret).  The component returns Pending and nil
//     so the reconcile loop can continue without surfacing a spurious error.
//
//   - *reconcileFailure — the component's own Reconcile step hit a typed error.
//     A warning event is emitted and the component returns Failed together with the
//     wrapped error so the caller can propagate it.
//
// Returns ok=false when the error was not classified — the caller should proceed
// with its normal observation logic.
func classifyReconcileErr(reconcileErr error, cond conditionTypes, events eventEmitter, obj client.Object, warningEvent, component string) (h componentHealth, err error, ok bool) {
	if errors.Is(reconcileErr, errContractsNotReady) {
		return newPendingHealth(cond, reasonUpstreamNotReady, msgUpstreamNotReady), nil, true
	}
	if rf, matched := errors.AsType[*reconcileFailure](reconcileErr); matched {
		events.emitWarning(obj, warningEvent, fmt.Sprintf("failed to reconcile %s for PostgresCluster %s — check operator logs", component, obj.GetName()))
		return newFailedHealth(cond, rf.reason, rf.err.Error()), rf.err, true
	}
	return componentHealth{}, nil, false
}

type eventEmitter interface {
	emitNormal(obj client.Object, reason, message string)
	emitWarning(obj client.Object, reason, message string)
}

type poolerEmitter interface {
	eventEmitter
	emitPoolerReadyTransition(obj client.Object, conditions []metav1.Condition)
	emitPoolerCreationTransition(obj client.Object, conditions []metav1.Condition)
}

func isIntermediateState(state pgcConstants.State) bool {
	switch state {
	case pgcConstants.Pending,
		pgcConstants.Provisioning,
		pgcConstants.Configuring:
		return true
	default:
		return false
	}
}

// setStatus sets the phase, condition and persists the status.
// It skips the API write when the resulting status is identical to the current
// state, avoiding unnecessary etcd churn and ResourceVersion bumps on stable clusters.
func setStatus(ctx context.Context, c client.Client, metrics ports.Recorder, cluster *enterprisev4.PostgresCluster, before *enterprisev4.PostgresClusterStatus, condType conditionTypes, status metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileClusterPhases) error {
	if phase != "" {
		p := string(phase)
		cluster.Status.Phase = &p
	}
	cluster.Status.ObservedGeneration = &cluster.Generation
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               string(condType),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})

	if equality.Semantic.DeepEqual(*before, cluster.Status) {
		return nil
	}

	if metrics != nil {
		metrics.IncStatusTransition(ports.ControllerCluster, string(condType), string(status), string(reason))
	}

	if err := c.Status().Update(ctx, cluster); err != nil {
		return fmt.Errorf("failed to update PostgresCluster status: %w", err)
	}
	return nil
}

func setStatusFromHealth(ctx context.Context, c client.Client, metrics ports.Recorder, cluster *enterprisev4.PostgresCluster, before *enterprisev4.PostgresClusterStatus, health componentHealth) error {
	conditionStatus := metav1.ConditionFalse
	if health.State == pgcConstants.Ready {
		conditionStatus = metav1.ConditionTrue
	}
	return setStatus(ctx, c, metrics, cluster, before, health.Condition, conditionStatus, health.Reason, health.Message, health.Phase)
}

func setPhaseStatus(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, phase reconcileClusterPhases) error {
	before := cluster.Status.DeepCopy()
	p := string(phase)
	cluster.Status.Phase = &p
	if equality.Semantic.DeepEqual(*before, cluster.Status) {
		return nil
	}
	if err := c.Status().Update(ctx, cluster); err != nil {
		return fmt.Errorf("failed to update PostgresCluster status phase: %w", err)
	}
	return nil
}

// deleteCNPGCluster deletes the CNPG Cluster if it exists.
func deleteCNPGCluster(ctx context.Context, c client.Client, cnpgCluster *cnpgv1.Cluster) error {
	logger := logging.FromContext(ctx).With("func", "deleteCNPGCluster")
	if cnpgCluster == nil {
		logger.InfoContext(ctx, "CNPG Cluster not found, skipping deletion")
		return nil
	}
	logger.InfoContext(ctx, "CNPG Cluster deletion started", "name", cnpgCluster.Name)
	if err := c.Delete(ctx, cnpgCluster); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting CNPG Cluster: %w", err)
	}
	return nil
}

// handleFinalizer processes deletion cleanup: removes poolers, then deletes or orphans the CNPG Cluster
// based on ClusterDeletionPolicy, then removes the finalizer.
func handleFinalizer(ctx context.Context, rc *ReconcileContext, cluster *enterprisev4.PostgresCluster) error {
	c := rc.Client
	scheme := rc.Scheme
	logger := logging.FromContext(ctx).With("func", "handleFinalizer")
	if cluster.GetDeletionTimestamp() == nil {
		logger.InfoContext(ctx, "PostgresCluster not marked for deletion, skipping finalizer logic")
		return nil
	}
	if !controllerutil.ContainsFinalizer(cluster, PostgresClusterFinalizerName) {
		logger.InfoContext(ctx, "finalizer not present on PostgresCluster, skipping finalizer logic")
		return nil
	}

	cnpgCluster := &cnpgv1.Cluster{}
	err := c.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cnpgCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cnpgCluster = nil
			logger.InfoContext(ctx, "CNPG cluster not found during cleanup")
		} else {
			return fmt.Errorf("fetching CNPG cluster: %w", err)
		}
	}
	logger.InfoContext(ctx, "finalizer cleanup started")

	policy := ""
	if cluster.Spec.ClusterDeletionPolicy != nil {
		policy = *cluster.Spec.ClusterDeletionPolicy
	}

	if err := deleteConnectionPoolers(ctx, c, cluster); err != nil {
		return fmt.Errorf("deleting connection poolers: %w", err)
	}

	switch policy {
	case clusterDeletionPolicyDelete:
		logger.InfoContext(ctx, "ClusterDeletionPolicy 'Delete', CNPG Cluster deletion started")
		if cnpgCluster != nil {
			if err := deleteCNPGCluster(ctx, c, cnpgCluster); err != nil {
				return fmt.Errorf("deleting CNPG Cluster: %w", err)
			}
		} else {
			logger.InfoContext(ctx, "CNPG Cluster not found, skipping deletion")
		}

	case clusterDeletionPolicyRetain:
		logger.InfoContext(ctx, "ClusterDeletionPolicy 'Retain', orphaning CNPG Cluster")
		if cnpgCluster != nil {
			originalCNPG := cnpgCluster.DeepCopy()
			refRemoved, err := removeOwnerRef(scheme, cluster, cnpgCluster)
			if err != nil {
				return fmt.Errorf("removing owner reference from CNPG cluster: %w", err)
			}
			if !refRemoved {
				logger.InfoContext(ctx, "owner reference already removed from CNPG Cluster, skipping patch")
			}
			// Strip the barman-cloud WAL archiver plugin before orphaning. The ObjectStore CR
			// it references is owned by this PostgresCluster and will be garbage-collected, so a
			// retained cluster would otherwise keep archiving WAL to a dangling config (failing
			// archiver, or — if the ObjectStore lingers — S3 growing unbounded with no owner to
			// reclaim it). This mirrors the volume-snapshot survivor, which retains its dormant
			// backup config but runs no active archiver. Existing S3 backups are untouched.
			if removeBarmanWALArchiverPlugin(cnpgCluster) {
				logger.InfoContext(ctx, "stripped barman-cloud WAL archiver plugin from retained CNPG Cluster")
			}
			if err := patchObject(ctx, c, originalCNPG, cnpgCluster, "CNPGCluster"); err != nil {
				return fmt.Errorf("patching CNPG cluster after removing owner reference: %w", err)
			}
			logger.InfoContext(ctx, "removed owner reference from CNPG Cluster")
		}

		// Remove owner reference from the superuser Secret to prevent cascading deletion.
		if cluster.Status.Resources != nil && cluster.Status.Resources.SuperUserSecretRef != nil {
			secretName := cluster.Status.Resources.SuperUserSecretRef.Name
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cluster.Namespace}, secret); err != nil {
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("fetching secret during cleanup: %w", err)
				}
				logger.InfoContext(ctx, "secret not found, skipping owner reference removal", "secret", secretName)
			} else {
				originalSecret := secret.DeepCopy()
				refRemoved, err := removeOwnerRef(scheme, cluster, secret)
				if err != nil {
					return fmt.Errorf("removing owner reference from Secret: %w", err)
				}
				if refRemoved {
					if err := patchObject(ctx, c, originalSecret, secret, "Secret"); err != nil {
						return fmt.Errorf("patching Secret after removing owner reference: %w", err)
					}
				}
				logger.InfoContext(ctx, "removed owner reference from Secret")
			}
		}

	default:
		return fmt.Errorf("unknown ClusterDeletionPolicy %q: must be %q or %q", policy, clusterDeletionPolicyDelete, clusterDeletionPolicyRetain)
	}

	controllerutil.RemoveFinalizer(cluster, PostgresClusterFinalizerName)
	if err := c.Update(ctx, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.InfoContext(ctx, "PostgresCluster already deleted, skipping finalizer update")
			return nil
		}
		return fmt.Errorf("removing finalizer: %w", err)
	}
	rc.emitNormal(cluster, EventCleanupComplete, fmt.Sprintf("cleanup complete for PostgresCluster %s (policy: %s)", cluster.Name, policy))
	logger.InfoContext(ctx, "finalizer removed, cleanup complete")
	return nil
}

func removeOwnerRef(scheme *runtime.Scheme, owner, obj client.Object) (bool, error) {
	hasRef, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), owner, scheme)
	if err != nil {
		return false, fmt.Errorf("checking owner reference: %w", err)
	}
	if !hasRef {
		return false, nil
	}
	if err := controllerutil.RemoveOwnerReference(owner, obj, scheme); err != nil {
		return false, fmt.Errorf("removing owner reference: %w", err)
	}
	return true, nil
}

// removeBarmanWALArchiverPlugin drops the operator-managed barman-cloud plugin entry from the
// CNPG cluster spec, leaving any plugins owned by other controllers/users intact. It returns
// true if an entry was removed. Used when orphaning a cluster on Retain so the survivor stops
// archiving WAL to an ObjectStore that is about to be garbage-collected.
func removeBarmanWALArchiverPlugin(cnpgCluster *cnpgv1.Cluster) bool {
	filtered := cnpgCluster.Spec.Plugins[:0:0]
	removed := false
	for _, p := range cnpgCluster.Spec.Plugins {
		if p.Name == barmanCloudPluginName {
			removed = true
			continue
		}
		filtered = append(filtered, p)
	}
	cnpgCluster.Spec.Plugins = filtered
	return removed
}

// patchObject patches obj from original; treats NotFound as a no-op.
func patchObject(ctx context.Context, c client.Client, original, obj client.Object, kind objectKind) error {
	if err := c.Patch(ctx, obj, client.MergeFrom(original)); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("patching %s: %w", kind, err)
	}
	return nil
}
