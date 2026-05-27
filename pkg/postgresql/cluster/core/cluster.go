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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"log/slog"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	password "github.com/sethvargo/go-password/password"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var errServerTLSLeafInvalid = errors.New("server TLS secret contains invalid certificate material")

// PostgresClusterService is the application service entry point called by the primary adapter (reconciler).
func PostgresClusterService(ctx context.Context, rc *ReconcileContext, req ctrl.Request) (ctrl.Result, error) {
	c := rc.Client
	logger := logging.FromContext(ctx).With("func", "PostgresClusterService")
	logger.DebugContext(ctx, "reconciling PostgresCluster")

	var cnpgCluster *cnpgv1.Cluster
	var poolerEnabled bool
	var postgresSecretName string
	secret := &corev1.Secret{}

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
		if err := setStatus(ctx, c, rc.Metrics, postgresCluster, conditionType, status, reason, message, phase); err != nil {
			return err
		}
		rc.emitClusterPhaseTransition(postgresCluster, oldPhase, currentPhase())
		return nil
	}
	updateComponentHealthStatus := func(health componentHealth) error {
		oldPhase := currentPhase()
		if err := setStatusFromHealth(ctx, c, rc.Metrics, postgresCluster, health); err != nil {
			return err
		}
		rc.emitClusterPhaseTransition(postgresCluster, oldPhase, currentPhase())
		return nil
	}
	updatePhaseStatus := func(phase reconcileClusterPhases) error {
		oldPhase := currentPhase()
		if err := setPhaseStatus(ctx, c, postgresCluster, phase); err != nil {
			return err
		}
		rc.emitClusterPhaseTransition(postgresCluster, oldPhase, currentPhase())
		return nil
	}

	// Finalizer handling must come before any other processing.
	if err := handleFinalizer(ctx, rc, postgresCluster, secret); err != nil {
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

	// Resolve or derive the superuser secret name.
	if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.SuperUserSecretRef != nil {
		postgresSecretName = postgresCluster.Status.Resources.SuperUserSecretRef.Name
		logger.InfoContext(ctx, "superuser secret resolved from status", "name", postgresSecretName)
	} else {
		postgresSecretName = fmt.Sprintf("%s%s", postgresCluster.Name, defaultSecretSuffix)
		logger.InfoContext(ctx, "superuser secret name derived", "name", postgresSecretName)
	}

	poolerEnabled = mergedConfig.Spec.ConnectionPoolerEnabled != nil && *mergedConfig.Spec.ConnectionPoolerEnabled
	poolerConfigPresent := mergedConfig.CNPG != nil && mergedConfig.CNPG.ConnectionPooler != nil
	backupEnabled := mergedConfig.Spec.Backup != nil && mergedConfig.Spec.Backup.Enabled != nil && *mergedConfig.Spec.Backup.Enabled
	backupConfigured := mergedConfig.CNPG != nil && mergedConfig.CNPG.Backup != nil && mergedConfig.CNPG.Backup.VolumeSnapshot != nil

	secretComponent := newSecretModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, postgresSecretName)
	clusterComponent := newClusterModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, clusterClass, mergedConfig, postgresSecretName)

	bootstrapManager := &componentManager{
		components: []component{
			secretComponent,
			clusterComponent,
		},
		logger: logger,
	}
	result, err := bootstrapManager.Handle(ctx)
	if err != nil {
		return result, err
	}
	if result != (ctrl.Result{}) {
		return result, nil
	}

	cnpgCluster = clusterComponent.cnpgCluster
	runtimeView := clusterRuntimeViewAdapter{model: clusterComponent}

	runtimeManager := &componentManager{
		components: []component{
			newManagedRolesModel(c, rc.Scheme, rc, updateComponentHealthStatus, runtimeView, postgresCluster, postgresSecretName),
			// clusterComponent satisfies SANPolicy (write) and ClusterRuntimeProbe (read);
			// passed twice so each side is independently mockable.
			newPoolerModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, clusterClass, mergedConfig, cnpgCluster, poolerEnabled, poolerConfigPresent, clusterComponent, clusterComponent),
			newBackupModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, mergedConfig, backupEnabled, backupConfigured),
			newConfigMapModel(c, rc.Scheme, rc, updateComponentHealthStatus, runtimeView, postgresCluster, postgresSecretName),
		},
		logger: logger,
	}

	result, err = runtimeManager.Handle(ctx)
	if err != nil {
		return result, err
	}
	if result != (ctrl.Result{}) {
		return result, nil
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

func isTransientError(err error) bool {
	return apierrors.IsConflict(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsTimeout(err)
}

func transientResult(err error) ctrl.Result {
	if apierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}
	}
	return ctrl.Result{RequeueAfter: retryDelay}
}

func writeComponentStatus(updateStatus healthStatusUpdater, health componentHealth) error {
	if updateStatus == nil {
		return nil
	}
	return updateStatus(health)
}

type componentManager struct {
	components []component
	logger     *slog.Logger
}

func (m *componentManager) Handle(ctx context.Context) (ctrl.Result, error) {
	for _, component := range m.components {
		componentLogger := m.logger.With("component", component.Name())
		gate := component.EvaluatePrerequisites(ctx)

		if gate.Allowed {
			component.Actuate(ctx)
		} else {
			componentLogger.InfoContext(ctx, "component blocked by prerequisites",
				"step", "prerequisites",
				"condition", gate.Health.Condition,
				"reason", gate.Health.Reason,
				"phase", gate.Health.Phase,
				"requeueAfter", gate.Health.Result.RequeueAfter)
		}

		health, err := component.Converge(ctx)
		if err != nil && isTransientError(err) {
			componentLogger.ErrorContext(ctx, "component convergence transient error, requeueing", "error", err, "step", "converge")
			return transientResult(err), nil
		}

		if err != nil {
			componentLogger.ErrorContext(ctx, "component convergence failed",
				"error", err,
				"step", "converge",
				"condition", health.Condition,
				"reason", health.Reason,
				"phase", health.Phase)
			return health.Result, fmt.Errorf("%s converge: %w", component.Name(), err)
		}
		if isIntermediateState(health.State) {
			componentLogger.InfoContext(ctx, "component convergence pending",
				"step", "converge",
				"condition", health.Condition,
				"reason", health.Reason,
				"phase", health.Phase,
				"requeueAfter", health.Result.RequeueAfter)
			return health.Result, nil
		}
		componentLogger.InfoContext(ctx, "component convergence ready",
			"step", "converge",
			"condition", health.Condition,
			"reason", health.Reason,
			"phase", health.Phase)
		if health.Result != (ctrl.Result{}) {
			componentLogger.InfoContext(ctx, "component requested explicit result",
				"step", "converge",
				"requeueAfter", health.Result.RequeueAfter)
			return health.Result, nil
		}
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

type component interface {
	Actuate(ctx context.Context)
	Converge(ctx context.Context) (componentHealth, error)
	EvaluatePrerequisites(ctx context.Context) prerequisiteDecision
	Name() string
}

type prerequisiteDecision struct {
	Allowed bool
	Health  componentHealth
}

type healthStatusUpdater func(health componentHealth) error

type eventEmitter interface {
	emitNormal(obj client.Object, reason, message string)
	emitWarning(obj client.Object, reason, message string)
}

type poolerEmitter interface {
	eventEmitter
	emitPoolerReadyTransition(obj client.Object, conditions []metav1.Condition)
	emitPoolerCreationTransition(obj client.Object, conditions []metav1.Condition)
}

type clusterRuntimeView interface {
	Cluster() *cnpgv1.Cluster
	IsHealthy() bool
	IsServerTLSLeafAlignedWithSpec(ctx context.Context) (bool, error)
}

type clusterRuntimeViewAdapter struct {
	model *clusterModel
}

func (v clusterRuntimeViewAdapter) Cluster() *cnpgv1.Cluster {
	return v.model.cnpgCluster
}

func (v clusterRuntimeViewAdapter) IsHealthy() bool {
	return v.model.cnpgCluster != nil && v.model.cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy
}

func (v clusterRuntimeViewAdapter) IsServerTLSLeafAlignedWithSpec(ctx context.Context) (bool, error) {
	return v.model.IsServerTLSLeafAlignedWithSpec(ctx)
}

type clusterModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	cluster      *enterprisev4.PostgresCluster
	clusterClass *enterprisev4.PostgresClusterClass
	mergedConfig *MergedConfig
	secretName   string
	cnpgCluster  *cnpgv1.Cluster
	cnpgCreated  bool
	// cnpgPatch classifies this reconcile's CNPG spec change. Converge uses
	// requiresPhaseGate() to decide whether to hold ClusterReady=Provisioning
	// while CNPG.Status.Phase still reflects the pre-patch value.
	cnpgPatch cnpgPatchKind

	metricsEnabled bool
	health         componentHealth
	actuateErr     error
}

func newClusterModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, clusterClass *enterprisev4.PostgresClusterClass, mergedConfig *MergedConfig, secretName string) *clusterModel {
	model := &clusterModel{
		client: c, scheme: scheme,
		events: events, updateStatus: updateStatus,
		cluster: cluster, clusterClass: clusterClass, mergedConfig: mergedConfig,
		secretName: secretName,
	}
	model.metricsEnabled = isPostgreSQLMetricsEnabled(cluster, clusterClass)
	return model
}

func (p *clusterModel) Name() string { return pgcConstants.ComponentProvisioner }

func (p *clusterModel) EvaluatePrerequisites(_ context.Context) prerequisiteDecision {
	if health, missing := p.getHealthOnMissingSecretRef(); missing {
		return prerequisiteDecision{
			Allowed: false,
			Health:  health,
		}
	}
	return prerequisiteDecision{Allowed: true}
}

func (p *clusterModel) Actuate(ctx context.Context) {
	p.actuateErr = nil
	p.cnpgCreated = false
	p.cnpgPatch = cnpgPatchNone

	desiredSpec := buildCNPGClusterSpec(p.mergedConfig, p.secretName, p.metricsEnabled)
	existingCNPG := &cnpgv1.Cluster{}
	err := p.client.Get(ctx, types.NamespacedName{Name: p.cluster.Name, Namespace: p.cluster.Namespace}, existingCNPG)
	if err != nil && !apierrors.IsNotFound(err) {
		p.health = componentHealth{
			State:   pgcConstants.Failed,
			Reason:  reasonClusterGetFailed,
			Message: fmt.Sprintf("Failed to get CNPG cluster: %v", err),
			Phase:   failedClusterPhase,
		}
		p.actuateErr = err
		return
	}

	if apierrors.IsNotFound(err) {
		newCluster, err := buildCNPGCluster(p.scheme, p.cluster, p.mergedConfig, p.secretName, p.metricsEnabled)
		if err != nil {
			p.events.emitWarning(p.cluster, EventClusterCreateFailed, fmt.Sprintf("failed to build CNPG cluster for PostgresCluster %s — check operator logs", p.cluster.Name))
			p.health = componentHealth{
				State:   pgcConstants.Failed,
				Reason:  reasonClusterBuildFailed,
				Message: fmt.Sprintf("failed to build CNPG cluster for PostgresCluster %s — check operator logs", p.cluster.Name),
				Phase:   failedClusterPhase,
			}
			p.actuateErr = err
			return
		}
		if err = p.client.Create(ctx, newCluster); err != nil {
			p.events.emitWarning(p.cluster, EventClusterCreateFailed, fmt.Sprintf("failed to create CNPG cluster for PostgresCluster %s — check operator logs", p.cluster.Name))
			p.health = componentHealth{
				State:   pgcConstants.Failed,
				Reason:  reasonClusterBuildFailed,
				Message: fmt.Sprintf("failed to create CNPG cluster for PostgresCluster %s — check operator logs", p.cluster.Name),
				Phase:   failedClusterPhase,
			}
			p.actuateErr = err
			return
		}
		p.events.emitNormal(p.cluster, EventClusterCreationStarted, fmt.Sprintf("CNPG cluster created for PostgresCluster %s, waiting for healthy state", p.cluster.Name))
		p.cnpgCluster = newCluster
		p.cnpgCreated = true
		return
	}

	p.cnpgCluster = existingCNPG
	hasOwnerRef, ownerRefErr := controllerutil.HasOwnerReference(p.cnpgCluster.GetOwnerReferences(), p.cluster, p.scheme)
	if ownerRefErr != nil {
		p.health = componentHealth{
			State:   pgcConstants.Failed,
			Reason:  reasonClusterGetFailed,
			Message: fmt.Sprintf("failed to check owner reference on CNPG cluster: %v", ownerRefErr),
			Phase:   failedClusterPhase,
		}
		p.actuateErr = fmt.Errorf("failed to check owner reference on CNPG cluster: %w", ownerRefErr)
		return
	}
	if !hasOwnerRef {
		originalCNPG := p.cnpgCluster.DeepCopy()
		if err := ctrl.SetControllerReference(p.cluster, p.cnpgCluster, p.scheme); err != nil {
			msg := fmt.Sprintf("failed to set controller reference on existing CNPG cluster: %v", err)
			p.health = componentHealth{State: pgcConstants.Failed, Reason: reasonClusterPatchFailed, Message: msg, Phase: failedClusterPhase}
			p.actuateErr = fmt.Errorf("failed to set controller reference on existing CNPG cluster: %w", err)
			return
		}
		if err := patchObject(ctx, p.client, originalCNPG, p.cnpgCluster, "CNPGCluster"); err != nil {
			msg := fmt.Sprintf("failed to adopt existing CNPG cluster for PostgresCluster %s — check operator logs", p.cluster.Name)
			p.events.emitWarning(p.cluster, EventClusterUpdateFailed, msg)
			p.health = componentHealth{State: pgcConstants.Failed, Reason: reasonClusterPatchFailed, Message: msg, Phase: failedClusterPhase}
			p.actuateErr = err
			return
		}
		p.events.emitNormal(p.cluster, EventClusterAdopted, fmt.Sprintf("Adopted existing CNPG cluster for PostgresCluster %s", p.cluster.Name))
		p.cnpgPatch = cnpgPatchBody
	}
	currentNormalized := normalizeCNPGClusterSpec(p.cnpgCluster.Spec, p.mergedConfig.Spec.PostgreSQLConfig)
	desiredNormalized := normalizeCNPGClusterSpec(desiredSpec, p.mergedConfig.Spec.PostgreSQLConfig)
	if !equality.Semantic.DeepEqual(currentNormalized, desiredNormalized) {
		originalCluster := p.cnpgCluster.DeepCopy()

		// Classify the drift BEFORE the patch is applied so Converge can decide
		// whether to gate ClusterReady on a CNPG phase transition.
		patchKind := cnpgPatchMetadata
		if isClusterDrift(currentNormalized, desiredNormalized) {
			patchKind = cnpgPatchBody
		}
		p.cnpgCluster.Spec = desiredSpec
		if err := patchObject(ctx, p.client, originalCluster, p.cnpgCluster, "CNPGCluster"); err != nil {
			msg := fmt.Sprintf("failed to patch CNPG cluster for PostgresCluster %s — check operator logs", p.cluster.Name)
			p.events.emitWarning(p.cluster, EventClusterUpdateFailed, msg)
			p.health = componentHealth{State: pgcConstants.Failed, Reason: reasonClusterPatchFailed, Message: msg, Phase: failedClusterPhase}
			p.actuateErr = err
			return
		}
		p.events.emitNormal(p.cluster, EventClusterUpdateStarted, fmt.Sprintf("CNPG cluster spec updated for PostgresCluster %s, waiting for healthy state", p.cluster.Name))
		p.cnpgPatch = patchKind
	}

	if p.cnpgCluster != nil {
		p.cluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: "postgresql.cnpg.io/v1",
			Kind:       "Cluster",
			Namespace:  p.cnpgCluster.Namespace,
			Name:       p.cnpgCluster.Name,
			UID:        p.cnpgCluster.UID,
		}
	}
}

func (p *clusterModel) Converge(_ context.Context) (health componentHealth, err error) {
	p.health.Condition = clusterReady
	defer func() {
		statusErr := writeComponentStatus(p.updateStatus, p.health)
		if statusErr != nil {
			if err != nil {
				err = errors.Join(err, statusErr)
			} else {
				err = statusErr
			}
		}
		health = p.health
	}()

	if missingHealth, missing := p.getHealthOnMissingSecretRef(); missing {
		p.health = missingHealth
		return p.health, nil
	}
	if p.actuateErr != nil {
		return p.health, p.actuateErr
	}

	if p.cnpgCluster == nil || p.cnpgCreated {
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Pending,
			Reason:    reasonCNPGProvisioning,
			Message:   msgCNPGPendingCreation,
			Phase:     pendingClusterPhase,
			Result:    ctrl.Result{RequeueAfter: retryDelay},
		}
		return p.health, nil
	}

	if p.cnpgPatch.requiresPhaseGate() && (p.cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy || p.cnpgCluster.Status.Phase == "") {
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Provisioning,
			Reason:    reasonCNPGProvisioning,
			Message:   fmt.Sprintf(msgFmtCNPGClusterPhase, p.cnpgCluster.Status.Phase),
			Phase:     provisioningClusterPhase,
			Result:    ctrl.Result{RequeueAfter: retryDelay},
		}
		return p.health, nil
	}

	requeue := ctrl.Result{RequeueAfter: retryDelay}
	phase := p.cnpgCluster.Status.Phase
	var convergeErr error

	switch phase {
	case cnpgv1.PhaseHealthy:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Ready,
			Reason:    reasonCNPGClusterHealthy,
			Message:   msgProvisionerHealthy,
			Phase:     readyClusterPhase,
		}
	case cnpgv1.PhaseFirstPrimary, cnpgv1.PhaseCreatingReplica, cnpgv1.PhaseWaitingForInstancesToBeActive:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Provisioning,
			Reason:    reasonCNPGProvisioning,
			Message:   fmt.Sprintf(msgFmtCNPGProvisioning, phase),
			Phase:     provisioningClusterPhase,
			Result:    requeue,
		}
	case cnpgv1.PhaseSwitchover:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Configuring,
			Reason:    reasonCNPGSwitchover,
			Message:   msgCNPGSwitchover,
			Phase:     configuringClusterPhase,
			Result:    requeue,
		}
	case cnpgv1.PhaseFailOver:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Configuring,
			Reason:    reasonCNPGFailingOver,
			Message:   msgCNPGFailingOver,
			Phase:     configuringClusterPhase,
			Result:    requeue,
		}
	case cnpgv1.PhaseInplacePrimaryRestart, cnpgv1.PhaseInplaceDeletePrimaryRestart:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Configuring,
			Reason:    reasonCNPGRestarting,
			Message:   fmt.Sprintf(msgFmtCNPGRestarting, phase),
			Phase:     configuringClusterPhase,
			Result:    requeue,
		}
	case cnpgv1.PhaseUpgrade, cnpgv1.PhaseMajorUpgrade, cnpgv1.PhaseUpgradeDelayed, cnpgv1.PhaseOnlineUpgrading:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Configuring,
			Reason:    reasonCNPGUpgrading,
			Message:   fmt.Sprintf(msgFmtCNPGUpgrading, phase),
			Phase:     configuringClusterPhase,
			Result:    requeue,
		}
	case cnpgv1.PhaseApplyingConfiguration:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Configuring,
			Reason:    reasonCNPGApplyingConfig,
			Message:   msgCNPGApplyingConfiguration,
			Phase:     configuringClusterPhase,
			Result:    requeue,
		}
	case cnpgv1.PhaseReplicaClusterPromotion:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Configuring,
			Reason:    reasonCNPGPromoting,
			Message:   msgCNPGPromoting,
			Phase:     configuringClusterPhase,
			Result:    requeue,
		}
	case cnpgv1.PhaseWaitingForUser:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Failed,
			Reason:    reasonCNPGWaitingForUser,
			Message:   msgCNPGWaitingForUser,
			Phase:     failedClusterPhase,
		}
		convergeErr = fmt.Errorf("provisioner requires user action")
	case cnpgv1.PhaseUnrecoverable:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Failed,
			Reason:    reasonCNPGUnrecoverable,
			Message:   msgCNPGUnrecoverable,
			Phase:     failedClusterPhase,
		}
		convergeErr = fmt.Errorf("provisioner unrecoverable")
	case cnpgv1.PhaseCannotCreateClusterObjects:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Failed,
			Reason:    reasonCNPGProvisioningFailed,
			Message:   msgCNPGCannotCreateObjects,
			Phase:     failedClusterPhase,
		}
		convergeErr = fmt.Errorf("provisioner cannot create cluster objects")
	case cnpgv1.PhaseUnknownPlugin, cnpgv1.PhaseFailurePlugin:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Failed,
			Reason:    reasonCNPGPluginError,
			Message:   fmt.Sprintf(msgFmtCNPGPluginError, phase),
			Phase:     failedClusterPhase,
		}
		convergeErr = fmt.Errorf("provisioner plugin error")
	case cnpgv1.PhaseImageCatalogError, cnpgv1.PhaseArchitectureBinaryMissing:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Failed,
			Reason:    reasonCNPGImageError,
			Message:   fmt.Sprintf(msgFmtCNPGImageError, phase),
			Phase:     failedClusterPhase,
		}
		convergeErr = fmt.Errorf("provisioner image error")
	case "":
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Pending,
			Reason:    reasonCNPGProvisioning,
			Message:   msgCNPGPendingCreation,
			Phase:     pendingClusterPhase,
			Result:    requeue,
		}
	default:
		p.health = componentHealth{
			Condition: clusterReady,
			State:     pgcConstants.Provisioning,
			Reason:    reasonCNPGProvisioning,
			Message:   fmt.Sprintf(msgFmtCNPGClusterPhase, phase),
			Phase:     provisioningClusterPhase,
			Result:    requeue,
		}
	}
	return p.health, convergeErr
}

// SANPolicy enforces and verifies the desired DNS identities on the underlying
// provisioned cluster (spec/desired only; runtime observation lives on
// ClusterRuntimeProbe).
type SANPolicy interface {
	EnsureSANPolicy(ctx context.Context) error
	IsSANPolicyConverged(ctx context.Context) (bool, error)
}

// ClusterRuntimeProbe reads materialized runtime state on the underlying
// provisioned cluster. Read-only by contract; mutating methods belong on a
// policy port. Intent-first naming keeps the adapter swappable.
type ClusterRuntimeProbe interface {
	// IsServerTLSLeafAlignedWithSpec reports whether the materialized server
	// TLS leaf carries every desired DNS name. poolerModel uses it to gate
	// readiness while the leaf cert lags spec convergence. See method docstring
	// for failure modes.
	IsServerTLSLeafAlignedWithSpec(ctx context.Context) (bool, error)
}

func (p *clusterModel) poolerEnabledFromMerged() bool {
	if p.mergedConfig == nil || p.mergedConfig.Spec == nil || p.mergedConfig.Spec.ConnectionPoolerEnabled == nil {
		return false
	}
	return *p.mergedConfig.Spec.ConnectionPoolerEnabled
}

func (p *clusterModel) getCNPGCluster(ctx context.Context) (*cnpgv1.Cluster, error) {
	key := types.NamespacedName{Name: p.cluster.Name, Namespace: p.cluster.Namespace}
	var cnpg cnpgv1.Cluster
	if err := p.client.Get(ctx, key, &cnpg); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &cnpg, nil
}

// computeDesiredPoolerSANSet returns the desired serverAltDNSNames sorted
// lexicographically (EnsureSANPolicy/IsSANPolicyConverged depend on stable
// order). Pooler-disabled preserves existing entries (union-on-enable) so a
// transient toggle does not trigger CNPG cert rotation.
func (p *clusterModel) computeDesiredPoolerSANSet(current []string, clusterName, namespace string) []string {
	set := make(map[string]struct{}, len(current))
	for _, s := range current {
		if s == "" {
			continue
		}
		set[s] = struct{}{}
	}

	poolerSANs := []string{
		fmt.Sprintf("%s.%s", poolerResourceName(clusterName, readWriteEndpoint), namespace),
		fmt.Sprintf("%s.%s%s", poolerResourceName(clusterName, readWriteEndpoint), namespace, poolerSANSuffix),
		fmt.Sprintf("%s.%s", poolerResourceName(clusterName, readOnlyEndpoint), namespace),
		fmt.Sprintf("%s.%s%s", poolerResourceName(clusterName, readOnlyEndpoint), namespace, poolerSANSuffix),
	}

	for _, s := range poolerSANs {
		set[s] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func getServerAltDNSNames(cnpg *cnpgv1.Cluster) []string {
	if cnpg == nil || cnpg.Spec.Certificates == nil {
		return nil
	}
	return cnpg.Spec.Certificates.ServerAltDNSNames
}

// EnsureSANPolicy applies a merge patch on CNPG Cluster certificates
// when pooler-related SANs drift from desired state.
// Short-circuits on pooler disabled/cnpg errors.
func (p *clusterModel) EnsureSANPolicy(ctx context.Context) error {
	if !p.poolerEnabledFromMerged() {
		return nil
	}
	cnpg, err := p.getCNPGCluster(ctx)
	if err != nil {
		return err
	}
	if cnpg == nil {
		return fmt.Errorf("cnpgv1.Cluster %s/%s not found while enforcing SAN policy; caller MUST gate on a non-nil cnpgCluster snapshot", p.cluster.Namespace, p.cluster.Name)
	}

	current := append([]string(nil), getServerAltDNSNames(cnpg)...)
	desired := p.computeDesiredPoolerSANSet(current, cnpg.Name, cnpg.Namespace)
	if sets.New(current...).Equal(sets.New(desired...)) {
		return nil
	}

	before := cnpg.DeepCopy()
	if cnpg.Spec.Certificates == nil {
		cnpg.Spec.Certificates = &cnpgv1.CertificatesConfiguration{}
	}
	cnpg.Spec.Certificates.ServerAltDNSNames = desired
	if err := p.client.Patch(ctx, cnpg, client.MergeFrom(before)); err != nil {
		return err
	}
	return nil
}

// IsSANPolicyConverged reports whether live CNPG serverAltDNSNames match the
// desired pooler SAN set (spec only). poolerModel additionally waits on
// ClusterRuntimeProbe.IsServerTLSLeafAlignedWithSpec so readiness does not
// advance while the leaf cert lags.
func (p *clusterModel) IsSANPolicyConverged(ctx context.Context) (bool, error) {
	if !p.poolerEnabledFromMerged() {
		return true, nil
	}
	cnpg, err := p.getCNPGCluster(ctx)
	if err != nil {
		return false, err
	}
	if cnpg == nil {
		return true, nil
	}

	current := sets.New(getServerAltDNSNames(cnpg)...)
	desired := sets.New(p.computeDesiredPoolerSANSet(current.UnsortedList(), cnpg.Name, cnpg.Namespace)...)
	return current.Equal(desired), nil
}

func serverTLSSecretNameFromCNPG(cnpg *cnpgv1.Cluster) string {
	if cnpg == nil {
		return ""
	}
	if cnpg.Status.Certificates.ServerTLSSecret != "" {
		return cnpg.Status.Certificates.ServerTLSSecret
	}
	if cnpg.Spec.Certificates != nil && cnpg.Spec.Certificates.ServerTLSSecret != "" {
		return cnpg.Spec.Certificates.ServerTLSSecret
	}
	return ""
}

// IsServerTLSLeafAlignedWithSpec satisfies ClusterRuntimeProbe. Failure modes:
//   - apiserver Get error          → (false, err) — propagated.
//   - no Cluster / no spec SANs    → (true,  nil).
//   - no Secret / no tls.crt       → (false, nil) — requeue (transient race with CNPG cert-controller).
//   - SAN mismatch                 → (false, nil) — requeue (normal mid-rotation).
//   - PEM/x509 parse failure       → (false, %w errServerTLSLeafInvalid) — caller demuxes via
//     errors.Is to surface reasonPoolerTLSLeafInvalidCert (Failed). Rich diagnostic detail
//     lives in the wrapped error for logs; do not echo it into events or Condition.Message.
func (p *clusterModel) IsServerTLSLeafAlignedWithSpec(ctx context.Context) (bool, error) {
	cnpg, err := p.getCNPGCluster(ctx)
	if err != nil {
		return false, err
	}
	if cnpg == nil {
		return true, nil
	}
	specSANs := getServerAltDNSNames(cnpg)
	if len(specSANs) == 0 {
		return true, nil
	}
	secretName := serverTLSSecretNameFromCNPG(cnpg)
	if secretName == "" {
		return false, nil
	}
	var sec corev1.Secret
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: p.cluster.Namespace, Name: secretName}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	raw := sec.Data[corev1.TLSCertKey]
	if len(raw) == 0 {
		return false, nil
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return false, fmt.Errorf("%w: PEM decode failed for secret %s/%s",
			errServerTLSLeafInvalid, p.cluster.Namespace, secretName)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("%w: x509 parse failed for secret %s/%s: %v",
			errServerTLSLeafInvalid, p.cluster.Namespace, secretName, err)
	}
	for _, alt := range specSANs {
		if alt == "" {
			continue
		}
		if !slices.Contains(cert.DNSNames, alt) {
			return false, nil
		}
	}
	return true, nil
}

func (p *clusterModel) getHealthOnMissingSecretRef() (componentHealth, bool) {
	if p.cluster.Status.Resources == nil || p.cluster.Status.Resources.SuperUserSecretRef == nil {
		return componentHealth{
			State:     pgcConstants.Pending,
			Condition: clusterReady,
			Reason:    reasonUserSecretPending,
			Message:   msgSecretRefNotPublished,
			Phase:     pendingClusterPhase,
			Result:    ctrl.Result{RequeueAfter: retryDelay},
		}, true
	}
	return componentHealth{}, false
}

type managedRolesModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	runtime      clusterRuntimeView
	cluster      *enterprisev4.PostgresCluster
	secret       string

	health     componentHealth
	actuateErr error
}

func newManagedRolesModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, runtime clusterRuntimeView, cluster *enterprisev4.PostgresCluster, secret string) *managedRolesModel {
	return &managedRolesModel{client: c, scheme: scheme, events: events, updateStatus: updateStatus, runtime: runtime, cluster: cluster, secret: secret}
}

func (m *managedRolesModel) Name() string { return pgcConstants.ComponentManagedRoles }

func (m *managedRolesModel) runtimeGateHealth() (componentHealth, bool) {
	if m.runtime == nil || !m.runtime.IsHealthy() {
		return componentHealth{
			State:     pgcConstants.Pending,
			Condition: managedRolesReady,
			Reason:    reasonManagedRolesPending,
			Message:   "Managed roles blocked until CNPG cluster is healthy",
			Phase:     pendingClusterPhase,
			Result:    ctrl.Result{RequeueAfter: retryDelay},
		}, true
	}
	return componentHealth{}, false
}

func (m *managedRolesModel) EvaluatePrerequisites(_ context.Context) prerequisiteDecision {
	if gateHealth, blocked := m.runtimeGateHealth(); blocked {
		return prerequisiteDecision{
			Allowed: false,
			Health:  gateHealth,
		}
	}
	return prerequisiteDecision{Allowed: true}
}

func (m *managedRolesModel) Actuate(ctx context.Context) {
	m.actuateErr = nil
	if err := reconcileManagedRoles(ctx, m.client, m.cluster, m.runtime.Cluster()); err != nil {
		msg := fmt.Sprintf("failed to reconcile managed roles for PostgresCluster %s — check operator logs", m.cluster.Name)
		m.events.emitWarning(m.cluster, EventManagedRolesFailed, msg)
		m.health = componentHealth{Condition: managedRolesReady, State: pgcConstants.Failed, Reason: reasonManagedRolesFailed, Message: msg, Phase: failedClusterPhase}
		m.actuateErr = err
		return
	}
}

func (m *managedRolesModel) Converge(ctx context.Context) (health componentHealth, err error) {
	_ = ctx
	defer func() {
		statusErr := writeComponentStatus(m.updateStatus, m.health)
		if statusErr != nil {
			if err != nil {
				err = errors.Join(err, statusErr)
			} else {
				err = statusErr
			}
		}
		health = m.health
	}()

	if gateHealth, blocked := m.runtimeGateHealth(); blocked {
		m.health = gateHealth
		return m.health, nil
	}
	if m.actuateErr != nil {
		return m.health, m.actuateErr
	}

	syncManagedRolesStatusFromCNPG(m.cluster, m.runtime.Cluster())
	status := m.cluster.Status.ManagedRolesStatus
	if status == nil {
		m.health = componentHealth{Condition: managedRolesReady, State: pgcConstants.Pending, Reason: reasonManagedRolesPending, Message: "Managed roles status not published yet", Phase: pendingClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return m.health, nil
	}

	if len(status.Failed) > 0 {
		m.health = componentHealth{Condition: managedRolesReady, State: pgcConstants.Failed, Reason: reasonManagedRolesFailed, Message: fmt.Sprintf("Managed roles reconciliation failed for %d role(s)", len(status.Failed)), Phase: failedClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		m.emitManagedRolesConvergeFailure(m.health.Message)
		return m.health, fmt.Errorf("managed roles have failed entries")
	}

	if len(status.Pending) > 0 {
		m.health = componentHealth{Condition: managedRolesReady, State: pgcConstants.Pending, Reason: reasonManagedRolesPending, Message: fmt.Sprintf("Managed roles pending for %d role(s)", len(status.Pending)), Phase: pendingClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return m.health, nil
	}

	m.health = componentHealth{Condition: managedRolesReady, State: pgcConstants.Ready, Reason: reasonManagedRolesReady, Message: "Managed roles are reconciled"}
	if !meta.IsStatusConditionTrue(m.cluster.Status.Conditions, string(managedRolesReady)) {
		m.events.emitNormal(m.cluster, EventManagedRolesReady, fmt.Sprintf("managed roles reconciled for PostgresCluster %s", m.cluster.Name))
	}
	return m.health, nil
}

func (m *managedRolesModel) emitManagedRolesConvergeFailure(message string) {
	cond := meta.FindStatusCondition(m.cluster.Status.Conditions, string(managedRolesReady))
	if cond != nil &&
		cond.Status == metav1.ConditionFalse &&
		cond.Reason == string(reasonManagedRolesFailed) &&
		cond.Message == message {
		return
	}
	m.events.emitWarning(m.cluster, EventManagedRolesFailed, message)
}

// TODO: Ports as access to cnpg originated info to decouple.
func syncManagedRolesStatusFromCNPG(cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) {
	if cluster == nil || cnpgCluster == nil {
		return
	}

	expectedRoles := make([]string, 0, len(cluster.Spec.ManagedRoles))
	for _, role := range cluster.Spec.ManagedRoles {
		expectedRoles = append(expectedRoles, role.Name)
	}

	cnpgStatus := cnpgCluster.Status.ManagedRolesStatus
	reconciled := append([]string(nil), cnpgStatus.ByStatus[cnpgv1.RoleStatusReconciled]...)
	pending := append([]string(nil), cnpgStatus.ByStatus[cnpgv1.RoleStatusPendingReconciliation]...)

	reconciledSet := make(map[string]struct{}, len(reconciled))
	for _, roleName := range reconciled {
		reconciledSet[roleName] = struct{}{}
	}
	pendingSet := make(map[string]struct{}, len(pending))
	for _, roleName := range pending {
		pendingSet[roleName] = struct{}{}
	}

	failed := make(map[string]string, len(cnpgStatus.CannotReconcile))
	for roleName, errs := range cnpgStatus.CannotReconcile {
		if len(errs) == 0 {
			failed[roleName] = "role cannot be reconciled"
			continue
		}
		failed[roleName] = strings.Join(errs, "; ")
	}

	for _, roleName := range expectedRoles {
		if _, ok := reconciledSet[roleName]; ok {
			continue
		}
		if _, ok := failed[roleName]; ok {
			continue
		}
		if _, ok := pendingSet[roleName]; ok {
			continue
		}
		pending = append(pending, roleName)
	}

	sort.Strings(reconciled)
	sort.Strings(pending)
	if len(failed) == 0 {
		failed = nil
	}

	cluster.Status.ManagedRolesStatus = &enterprisev4.ManagedRolesStatus{
		Reconciled: reconciled,
		Pending:    pending,
		Failed:     failed,
	}
}

type poolerModel struct {
	client              client.Client
	scheme              *runtime.Scheme
	events              poolerEmitter
	updateStatus        healthStatusUpdater
	cluster             *enterprisev4.PostgresCluster
	clusterClass        *enterprisev4.PostgresClusterClass
	mergedConfig        *MergedConfig
	cnpgCluster         *cnpgv1.Cluster
	poolerEnabled       bool
	poolerConfigPresent bool

	metricsEnabled bool
	health         componentHealth
	actuateErr     error

	// Write side (desired SAN spec) and read side (materialized runtime state).
	// Usually one *clusterModel satisfies both — split into two fields so the
	// spec-vs-runtime axis is first-class at the type level.
	sanPolicy    SANPolicy
	runtimeProbe ClusterRuntimeProbe
}

func newPoolerModel(c client.Client, scheme *runtime.Scheme, events poolerEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, clusterClass *enterprisev4.PostgresClusterClass, mergedConfig *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerEnabled bool, poolerConfigPresent bool, sanPolicy SANPolicy, runtimeProbe ClusterRuntimeProbe) *poolerModel {
	model := &poolerModel{
		client:              c,
		scheme:              scheme,
		events:              events,
		updateStatus:        updateStatus,
		cluster:             cluster,
		clusterClass:        clusterClass,
		mergedConfig:        mergedConfig,
		cnpgCluster:         cnpgCluster,
		poolerEnabled:       poolerEnabled,
		poolerConfigPresent: poolerConfigPresent,
		sanPolicy:           sanPolicy,
		runtimeProbe:        runtimeProbe,
	}
	model.metricsEnabled = isConnectionPoolerMetricsEnabled(cluster, clusterClass)
	return model
}

func (p *poolerModel) Name() string { return pgcConstants.ComponentPooler }

func (p *poolerModel) EvaluatePrerequisites(_ context.Context) prerequisiteDecision {
	if !p.poolerEnabled || !p.poolerConfigPresent {
		return prerequisiteDecision{Allowed: true}
	}
	if p.cnpgCluster == nil {
		return prerequisiteDecision{
			Allowed: false,
			Health: componentHealth{
				State:     pgcConstants.Pending,
				Condition: poolerReady,
				Reason:    reasonCNPGProvisioning,
				Message:   msgCNPGPendingCreation,
				Phase:     pendingClusterPhase,
				Result:    ctrl.Result{RequeueAfter: retryDelay},
			},
		}
	}
	if p.cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
		return prerequisiteDecision{
			Allowed: false,
			Health: componentHealth{
				State:     pgcConstants.Provisioning,
				Condition: poolerReady,
				Reason:    reasonCNPGProvisioning,
				Message:   fmt.Sprintf(msgFmtCNPGClusterPhase, p.cnpgCluster.Status.Phase),
				Phase:     provisioningClusterPhase,
				Result:    ctrl.Result{RequeueAfter: retryDelay},
			},
		}
	}
	return prerequisiteDecision{Allowed: true}
}

func (p *poolerModel) Actuate(ctx context.Context) {
	p.actuateErr = nil
	switch {
	case !p.poolerEnabled:
		if err := deleteConnectionPoolers(ctx, p.client, p.cluster); err != nil {
			p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerReconciliationFailed, Message: fmt.Sprintf("Failed to delete poolers: %v", err), Phase: failedClusterPhase}
			p.actuateErr = err
			return
		}
		p.cluster.Status.ConnectionPoolerStatus = nil
		meta.RemoveStatusCondition(&p.cluster.Status.Conditions, string(poolerReady))
		return
	case !p.poolerConfigPresent:
		return
	case p.cnpgCluster == nil || p.cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy:
		return
	default:
		if err := p.sanPolicy.EnsureSANPolicy(ctx); err != nil {
			msg := fmt.Sprintf("failed to reconcile pooler SAN policy for PostgresCluster %s — check operator logs", p.cluster.Name)
			p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
			p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerReconciliationFailed, Message: msg, Phase: failedClusterPhase}
			p.actuateErr = err
			return
		}

		if err := createOrUpdateConnectionPoolers(ctx, p.client, p.scheme, p.cluster, p.mergedConfig, p.cnpgCluster, p.metricsEnabled); err != nil {
			msg := fmt.Sprintf("failed to reconcile connection pooler for PostgresCluster %s — check operator logs", p.cluster.Name)
			p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
			p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerReconciliationFailed, Message: msg, Phase: failedClusterPhase}
			p.actuateErr = err
			return
		}
		return
	}
}

func (p *poolerModel) Converge(ctx context.Context) (health componentHealth, err error) {
	oldConditions := append([]metav1.Condition(nil), p.cluster.Status.Conditions...)
	defer func() {
		statusErr := writeComponentStatus(p.updateStatus, p.health)
		if statusErr != nil {
			if err != nil {
				err = errors.Join(err, statusErr)
			} else {
				err = statusErr
			}
		}
		health = p.health
	}()

	if p.actuateErr != nil {
		return p.health, p.actuateErr
	}

	if !p.poolerEnabled {
		// IsSANPolicyConverged short-circuits to (true, nil) when the pooler is off;
		// the err/!converged branches below are defensive (survive failing mocks).
		converged, err := p.sanPolicy.IsSANPolicyConverged(ctx)
		if err != nil {
			p.health = componentHealth{
				Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerReconciliationFailed,
				Message: fmt.Sprintf("failed to verify pooler SAN policy for PostgresCluster %s — check operator logs", p.cluster.Name),
				Phase:   failedClusterPhase}
			return p.health, err
		}
		if !converged {
			p.health = componentHealth{
				Condition: poolerReady, State: pgcConstants.Provisioning,
				Reason: reasonPoolerSANsPending, Message: msgPoolerSANsPending,
				Phase: provisioningClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
			return p.health, nil
		}
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Ready, Reason: reasonPoolerDisabled, Message: msgPoolerDisabled, Phase: readyClusterPhase}
		return p.health, nil
	}
	if !p.poolerConfigPresent {
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerConfigMissing, Message: msgPoolerConfigMissing, Phase: failedClusterPhase}
		return p.health, fmt.Errorf("pooler config missing")
	}
	if p.cnpgCluster == nil {
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Pending, Reason: reasonCNPGProvisioning, Message: msgCNPGPendingCreation, Phase: pendingClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return p.health, nil
	}
	if p.cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Provisioning, Reason: reasonCNPGProvisioning, Message: fmt.Sprintf(msgFmtCNPGClusterPhase, p.cnpgCluster.Status.Phase), Phase: provisioningClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return p.health, nil
	}

	handleSanCheck := func(b bool, err error, errMsg string, pendingReason conditionReasons, pendingMsg string) (bool, error) {
		if err != nil {
			p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, errMsg)
			p.health = componentHealth{
				Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerReconciliationFailed,
				Message: errMsg, Phase: failedClusterPhase}
			return true, err
		}
		if !b {
			p.health = componentHealth{
				Condition: poolerReady, State: pgcConstants.Provisioning,
				Reason: pendingReason, Message: pendingMsg,
				Phase: provisioningClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
			return true, nil
		}
		return false, nil
	}

	sanConverged, sanErr := p.sanPolicy.IsSANPolicyConverged(ctx)
	if errorOccurred, err := handleSanCheck(sanConverged, sanErr,
		fmt.Sprintf("failed to verify pooler SAN policy for PostgresCluster %s — check operator logs", p.cluster.Name),
		reasonPoolerSANsPending, msgPoolerSANsPending); errorOccurred {
		return p.health, err
	}

	// Read-side port: observe materialized leaf, never patch.
	leafOK, leafErr := p.runtimeProbe.IsServerTLSLeafAlignedWithSpec(ctx)
	// Structural failure (malformed PEM / x509) → Failed condition with a
	// distinct reason so the user sees what's wrong instead of an indefinite
	// PoolerTLSLeafPending loop. Rich detail stays in the log; event +
	// Condition.Message stay scrubbed and stable.
	if errors.Is(leafErr, errServerTLSLeafInvalid) {
		logger := logging.FromContext(ctx)
		secretName := serverTLSSecretNameFromCNPG(p.cnpgCluster)
		logger.Error("server TLS secret cannot be parsed; cluster requires investigation",
			"error", leafErr.Error(),
			"namespace", p.cluster.Namespace,
			"pgCluster", p.cluster.Name,
			"secret", secretName,
		)
		msg := fmt.Sprintf(string(msgFmtPoolerTLSLeafInvalidCert), p.cluster.Namespace, secretName)
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
		p.health = componentHealth{
			Condition: poolerReady,
			State:     pgcConstants.Failed,
			Reason:    reasonPoolerTLSLeafInvalidCert,
			Message:   msg,
			Phase:     failedClusterPhase,
		}
		return p.health, leafErr
	}
	if errorOccurred, err := handleSanCheck(leafOK, leafErr,
		fmt.Sprintf("failed to verify server TLS leaf for PostgresCluster %s — check operator logs", p.cluster.Name),
		reasonPoolerTLSLeafPending, msgPoolerTLSLeafPending); errorOccurred {
		return p.health, err
	}

	// TODO: Port material.
	rwExists, err := poolerExists(ctx, p.client, p.cluster, readWriteEndpoint)
	if err != nil {
		msg := fmt.Sprintf("failed to sync pooler status for PostgresCluster %s — check operator logs", p.cluster.Name)
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerReconciliationFailed, Message: fmt.Sprintf("Failed to check RW pooler existence: %v", err), Phase: failedClusterPhase}
		return p.health, err
	}
	roExists, err := poolerExists(ctx, p.client, p.cluster, readOnlyEndpoint)
	if err != nil {
		msg := fmt.Sprintf("failed to sync pooler status for PostgresCluster %s — check operator logs", p.cluster.Name)
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Failed, Reason: reasonPoolerReconciliationFailed, Message: fmt.Sprintf("Failed to check RO pooler existence: %v", err), Phase: failedClusterPhase}
		return p.health, err
	}
	if !rwExists || !roExists {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Provisioning, Reason: reasonPoolerCreating, Message: msgPoolersProvisioning, Phase: provisioningClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return p.health, nil
	}

	rwPooler := &cnpgv1.Pooler{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(p.cluster.Name, readWriteEndpoint),
		Namespace: p.cluster.Namespace,
	}, rwPooler); err != nil {
		if !apierrors.IsNotFound(err) {
			return componentHealth{Condition: poolerReady, State: pgcConstants.Failed}, fmt.Errorf("getting RW pooler: %w", err)
		}
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Pending, Reason: reasonPoolerCreating, Message: msgWaitRWPoolerObject, Phase: pendingClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return p.health, nil
	}
	roPooler := &cnpgv1.Pooler{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(p.cluster.Name, readOnlyEndpoint),
		Namespace: p.cluster.Namespace,
	}, roPooler); err != nil {
		if !apierrors.IsNotFound(err) {
			return componentHealth{Condition: poolerReady, State: pgcConstants.Failed}, fmt.Errorf("getting RO pooler: %w", err)
		}
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Pending, Reason: reasonPoolerCreating, Message: msgWaitROPoolerObject, Phase: pendingClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return p.health, nil
	}
	if !arePoolersReady(rwPooler, roPooler) {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Pending, Reason: reasonPoolerCreating, Message: msgPoolersNotReady, Phase: pendingClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return p.health, nil
	}

	p.cluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{Enabled: true}
	p.health = componentHealth{Condition: poolerReady, State: pgcConstants.Ready, Reason: reasonAllInstancesReady, Message: msgPoolersReady}
	p.events.emitPoolerReadyTransition(p.cluster, oldConditions)
	return p.health, nil
}

type configMapModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	runtime      clusterRuntimeView
	cluster      *enterprisev4.PostgresCluster
	secret       string

	health     componentHealth
	actuateErr error
}

func newConfigMapModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, runtime clusterRuntimeView, cluster *enterprisev4.PostgresCluster, secret string) *configMapModel {
	return &configMapModel{client: c, scheme: scheme, events: events, updateStatus: updateStatus, runtime: runtime, cluster: cluster, secret: secret}
}

func (c *configMapModel) Name() string { return pgcConstants.ComponentConfigMap }

func (c *configMapModel) EvaluatePrerequisites(_ context.Context) prerequisiteDecision {
	return prerequisiteDecision{Allowed: true}
}

func (c *configMapModel) Actuate(ctx context.Context) {
	c.actuateErr = nil
	cnpgCluster := c.runtime.Cluster()
	if cnpgCluster == nil {
		return
	}
	// Probe errors degrade to "not aligned"; poolerModel owns the user-facing
	// PoolerReady routing, so configMapModel just suppresses the keys.
	tlsLeafAligned, leafErr := c.runtime.IsServerTLSLeafAlignedWithSpec(ctx)
	if leafErr != nil {
		tlsLeafAligned = false
	}
	desiredCM, err := generateConfigMap(ctx, c.client, c.scheme, c.cluster, cnpgCluster, c.secret, tlsLeafAligned)
	if err != nil {
		c.events.emitWarning(c.cluster, EventConfigMapReconcileFailed, fmt.Sprintf("failed to reconcile ConfigMap for PostgresCluster %s — check operator logs", c.cluster.Name))
		c.health.State = pgcConstants.Failed
		c.health.Reason = reasonConfigMapFailed
		c.health.Message = fmt.Sprintf("failed to reconcile ConfigMap for PostgresCluster %s — check operator logs", c.cluster.Name)
		c.health.Phase = failedClusterPhase
		c.health.Result = ctrl.Result{}
		c.actuateErr = err
		return
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desiredCM.Name, Namespace: desiredCM.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, c.client, cm, func() error {
		cm.Data = desiredCM.Data
		cm.Annotations = desiredCM.Annotations
		cm.Labels = desiredCM.Labels
		if !metav1.IsControlledBy(cm, c.cluster) {
			if setErr := ctrl.SetControllerReference(c.cluster, cm, c.scheme); setErr != nil {
				return fmt.Errorf("setting controller reference: %w", setErr)
			}
		}
		return nil
	})
	if err != nil {
		c.events.emitWarning(c.cluster, EventConfigMapReconcileFailed, fmt.Sprintf("failed to reconcile ConfigMap for PostgresCluster %s — check operator logs", c.cluster.Name))
		c.health.State = pgcConstants.Failed
		c.health.Reason = reasonConfigMapFailed
		c.health.Message = fmt.Sprintf("failed to reconcile ConfigMap for PostgresCluster %s — check operator logs", c.cluster.Name)
		c.health.Phase = failedClusterPhase
		c.health.Result = ctrl.Result{}
		c.actuateErr = err
		return
	}
	switch op {
	case controllerutil.OperationResultCreated:
		c.events.emitNormal(c.cluster, EventConfigMapReconciled, fmt.Sprintf("ConfigMap %s created", desiredCM.Name))
	case controllerutil.OperationResultUpdated:
		c.events.emitNormal(c.cluster, EventConfigMapReconciled, fmt.Sprintf("ConfigMap %s updated", desiredCM.Name))
	}
	if c.cluster.Status.Resources == nil {
		c.cluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}
	if c.cluster.Status.Resources.ConfigMapRef == nil {
		c.cluster.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: desiredCM.Name}
	}
}

func (c *configMapModel) Converge(ctx context.Context) (health componentHealth, err error) {
	c.health.Condition = configMapsReady
	defer func() {
		statusErr := writeComponentStatus(c.updateStatus, c.health)
		if statusErr != nil {
			if err != nil {
				err = errors.Join(err, statusErr)
			} else {
				err = statusErr
			}
		}
		health = c.health
	}()

	if c.runtime == nil || !c.runtime.IsHealthy() {
		c.health.State = pgcConstants.Provisioning
		c.health.Reason = reasonCNPGProvisioning
		c.health.Message = msgCNPGPendingCreation
		c.health.Phase = provisioningClusterPhase
		c.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return c.health, nil
	}
	if c.actuateErr != nil {
		return c.health, c.actuateErr
	}

	if c.cluster.Status.Resources == nil || c.cluster.Status.Resources.ConfigMapRef == nil {
		c.health.State = pgcConstants.Provisioning
		c.health.Reason = reasonConfigMapFailed
		c.health.Message = msgConfigMapRefNotPublished
		c.health.Phase = provisioningClusterPhase
		c.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return c.health, nil
	}

	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: c.cluster.Status.Resources.ConfigMapRef.Name, Namespace: c.cluster.Namespace}
	if err := c.client.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) {
			c.health.State = pgcConstants.Provisioning
			c.health.Reason = reasonConfigMapFailed
			c.health.Message = msgConfigMapNotFoundYet
			c.health.Phase = provisioningClusterPhase
			c.health.Result = ctrl.Result{RequeueAfter: retryDelay}
			return c.health, nil
		}
		c.health.State = pgcConstants.Failed
		c.health.Reason = reasonConfigMapFailed
		c.health.Message = fmt.Sprintf("Failed to fetch ConfigMap: %v", err)
		c.health.Phase = failedClusterPhase
		c.health.Result = ctrl.Result{}
		return c.health, err
	}

	requiredKeys := []string{
		configKeyClusterRWEndpoint,
		configKeyClusterROEndpoint,
		configKeyClusterREndpoint,
		configKeyDefaultClusterPort,
		configKeySuperUserSecretRef,
	}
	for _, requiredKey := range requiredKeys {
		if _, ok := cm.Data[requiredKey]; !ok {
			c.health.State = pgcConstants.Failed
			c.health.Reason = reasonConfigMapFailed
			c.health.Message = fmt.Sprintf(msgFmtConfigMapMissingRequiredKey, requiredKey)
			c.health.Phase = failedClusterPhase
			c.health.Result = ctrl.Result{}
			return c.health, fmt.Errorf("configmap missing key %s", requiredKey)
		}
	}

	// CA discovery race: CNPG may publish ServerCASecret before the access
	// ConfigMap exposes it. Requeue on missing SERVER_CA_SECRET_REF so we
	// don't settle Ready without the CA discovery fields.
	if _, ok := cm.Data[configKeyServerCASecretRef]; !ok {
		c.health.State = pgcConstants.Provisioning
		c.health.Reason = reasonConfigMapFailed
		c.health.Message = msgConfigMapCAMetadataPending
		c.health.Phase = provisioningClusterPhase
		c.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return c.health, nil
	}

	c.health.State = pgcConstants.Ready
	c.health.Reason = reasonConfigMapReady
	c.health.Message = msgAccessConfigMapReady
	c.health.Result = ctrl.Result{}
	if !meta.IsStatusConditionTrue(c.cluster.Status.Conditions, string(configMapsReady)) {
		c.events.emitNormal(c.cluster, EventConfigMapReady, c.health.Message)
	}
	return c.health, nil
}

type secretModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	cluster      *enterprisev4.PostgresCluster
	name         string

	health     componentHealth
	actuateErr error
}

func newSecretModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, name string) *secretModel {
	return &secretModel{client: c, scheme: scheme, events: events, updateStatus: updateStatus, cluster: cluster, name: name}
}

func (s *secretModel) Name() string { return pgcConstants.ComponentSecret }

func (s *secretModel) EvaluatePrerequisites(_ context.Context) prerequisiteDecision {
	return prerequisiteDecision{Allowed: true}
}

func (s *secretModel) Actuate(ctx context.Context) {
	s.actuateErr = nil
	secret := &corev1.Secret{}
	secretExists, secretErr := clusterSecretExists(ctx, s.client, s.cluster.Namespace, s.name, secret)
	if secretErr != nil {
		msg := fmt.Sprintf("failed to check secret existence for PostgresCluster %s — check operator logs", s.cluster.Name)
		s.events.emitWarning(s.cluster, EventSecretReconcileFailed, msg)
		s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Failed, Reason: reasonSuperUserSecretFailed, Message: msg, Phase: failedClusterPhase}
		s.actuateErr = secretErr
		return
	}
	if !secretExists {
		if err := ensureClusterSecret(ctx, s.client, s.scheme, s.cluster, s.name); err != nil {
			msg := fmt.Sprintf("failed to generate cluster secret for PostgresCluster %s — check operator logs", s.cluster.Name)
			s.events.emitWarning(s.cluster, EventSecretReconcileFailed, msg)
			s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Failed, Reason: reasonSuperUserSecretFailed, Message: msg, Phase: failedClusterPhase}
			s.actuateErr = err
			return
		}
	}
	hasOwnerRef, ownerRefErr := controllerutil.HasOwnerReference(secret.GetOwnerReferences(), s.cluster, s.scheme)
	if ownerRefErr != nil {
		msg := fmt.Sprintf("failed to check owner reference on secret: %v", ownerRefErr)
		s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Failed, Reason: reasonSuperUserSecretFailed, Message: msg, Phase: failedClusterPhase}
		s.actuateErr = fmt.Errorf("failed to check owner reference on secret: %w", ownerRefErr)
		return
	}
	if secretExists && !hasOwnerRef {
		originalSecret := secret.DeepCopy()
		if err := ctrl.SetControllerReference(s.cluster, secret, s.scheme); err != nil {
			msg := fmt.Sprintf("failed to set controller reference on existing secret: %v", err)
			s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Failed, Reason: reasonSuperUserSecretFailed, Message: msg, Phase: failedClusterPhase}
			s.actuateErr = fmt.Errorf("failed to set controller reference on existing secret: %w", err)
			return
		}
		if err := patchObject(ctx, s.client, originalSecret, secret, "Secret"); err != nil {
			msg := fmt.Sprintf("failed to patch existing secret for PostgresCluster %s — check operator logs", s.cluster.Name)
			s.events.emitWarning(s.cluster, EventSecretReconcileFailed, msg)
			s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Failed, Reason: reasonSuperUserSecretFailed, Message: msg, Phase: failedClusterPhase}
			s.actuateErr = err
			return
		}
		s.events.emitNormal(s.cluster, EventClusterAdopted, fmt.Sprintf("Adopted existing CNPG cluster and secret %s", s.name))
	}
	if s.cluster.Status.Resources.SuperUserSecretRef == nil {
		s.cluster.Status.Resources.SuperUserSecretRef = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: s.name},
			Key:                  secretKeyPassword,
		}
	}
}

func (s *secretModel) Converge(ctx context.Context) (health componentHealth, err error) {
	defer func() {
		statusErr := writeComponentStatus(s.updateStatus, s.health)
		if statusErr != nil {
			if err != nil {
				err = errors.Join(err, statusErr)
			} else {
				err = statusErr
			}
		}
		health = s.health
	}()

	if s.actuateErr != nil {
		return s.health, s.actuateErr
	}

	if s.cluster.Status.Resources == nil || s.cluster.Status.Resources.SuperUserSecretRef == nil {
		s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Provisioning, Reason: reasonUserSecretPending, Message: msgSecretRefNotPublished, Phase: provisioningClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
		return s.health, nil
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: s.cluster.Status.Resources.SuperUserSecretRef.Name, Namespace: s.cluster.Namespace}
	if err := s.client.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Provisioning, Reason: reasonUserSecretPending, Message: msgSecretNotFoundYet, Phase: provisioningClusterPhase, Result: ctrl.Result{RequeueAfter: retryDelay}}
			return s.health, nil
		}
		s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Failed, Reason: reasonUserSecretFailed, Message: fmt.Sprintf("Failed to fetch superuser secret: %v", err), Phase: failedClusterPhase}
		return s.health, err
	}

	refKey := s.cluster.Status.Resources.SuperUserSecretRef.Key
	if refKey == "" {
		refKey = secretKeyPassword
	}
	if _, ok := secret.Data[refKey]; !ok {
		s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Failed, Reason: reasonSuperUserSecretFailed, Message: fmt.Sprintf(msgFmtSecretMissingKey, refKey), Phase: failedClusterPhase}
		return s.health, fmt.Errorf("secret missing key %s", refKey)
	}

	s.health = componentHealth{Condition: secretsReady, State: pgcConstants.Ready, Reason: reasonSuperUserSecretReady, Message: msgSuperuserSecretReady}
	if !meta.IsStatusConditionTrue(s.cluster.Status.Conditions, string(secretsReady)) {
		s.events.emitNormal(s.cluster, EventSecretReady, s.health.Message)
	}
	return s.health, nil
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

// GetMergedConfig overlays PostgresCluster spec on top of the class defaults.
// Class values are used only where the cluster spec is silent.
// Returns the merged config without validation — call ValidateMergedConfig separately.
func GetMergedConfig(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) *MergedConfig {
	result := cluster.Spec.DeepCopy()

	// Config is optional on the class — apply defaults only when provided.
	if defaults := class.Spec.Config; defaults != nil {
		if result.Instances == nil {
			result.Instances = defaults.Instances
		}
		if result.PostgresVersion == nil {
			result.PostgresVersion = defaults.PostgresVersion
		}
		if result.Resources == nil {
			result.Resources = defaults.Resources
		}
		if result.Storage == nil {
			result.Storage = defaults.Storage
		}
		if len(result.PostgreSQLConfig) == 0 {
			result.PostgreSQLConfig = defaults.PostgreSQLConfig
		}
		if len(result.PgHBA) == 0 {
			result.PgHBA = defaults.PgHBA
		}
		if result.ConnectionPoolerEnabled == nil {
			result.ConnectionPoolerEnabled = defaults.ConnectionPoolerEnabled
		}
		if defaults.Backup != nil {
			if result.Backup == nil {
				result.Backup = defaults.Backup.DeepCopy()
			} else {
				if result.Backup.Enabled == nil {
					result.Backup.Enabled = defaults.Backup.Enabled
				}
				if result.Backup.Schedule == nil {
					result.Backup.Schedule = defaults.Backup.Schedule
				}
			}
		}
	}

	if result.PostgreSQLConfig == nil {
		result.PostgreSQLConfig = make(map[string]string)
	}
	if result.PgHBA == nil {
		result.PgHBA = make([]string, 0)
	}
	if result.Resources == nil {
		result.Resources = &corev1.ResourceRequirements{}
	}

	return &MergedConfig{Spec: result, CNPG: class.Spec.CNPG}
}

// ValidateCrossResource checks constraints that require both the class and the cluster to be visible.
// It is called from both the webhook (admission) and the reconciler (runtime fallback).
func ValidateCrossResource(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) []ConfigValidationError {
	var errs []ConfigValidationError

	if classConfig := class.Spec.Config; classConfig != nil {
		if cluster.Spec.PostgresVersion != nil && classConfig.PostgresVersion != nil {
			clusterMajor, clusterMinor := parseVersion(*cluster.Spec.PostgresVersion)
			classMajor, classMinor := parseVersion(*classConfig.PostgresVersion)
			if clusterMinor < 0 {
				clusterMinor = 0
			}
			if clusterMajor > 0 && classMajor > 0 {
				versionTooLow := clusterMajor < classMajor ||
					(clusterMajor == classMajor && classMinor >= 0 && clusterMinor < classMinor)
				if versionTooLow {
					errs = append(errs, ConfigValidationError{
						Field:   "spec.postgresVersion",
						Value:   *cluster.Spec.PostgresVersion,
						Message: "postgresVersion cannot be lower than class default (" + *classConfig.PostgresVersion + ")",
					})
				}
			}
		}
	}

	poolerEnabled := (cluster.Spec.ConnectionPoolerEnabled != nil && *cluster.Spec.ConnectionPoolerEnabled) ||
		(class.Spec.Config != nil && class.Spec.Config.ConnectionPoolerEnabled != nil && *class.Spec.Config.ConnectionPoolerEnabled)
	if poolerEnabled && (class.Spec.CNPG == nil || class.Spec.CNPG.ConnectionPooler == nil) {
		errs = append(errs, ConfigValidationError{
			Field:   "spec.connectionPoolerEnabled",
			Value:   true,
			Message: "connection pooler requires cnpg.connectionPooler configuration in PostgresClusterClass",
		})
	}

	backupEnabled := (cluster.Spec.Backup != nil && cluster.Spec.Backup.Enabled != nil && *cluster.Spec.Backup.Enabled) ||
		(class.Spec.Config != nil && class.Spec.Config.Backup != nil && class.Spec.Config.Backup.Enabled != nil && *class.Spec.Config.Backup.Enabled)
	if backupEnabled && (class.Spec.CNPG == nil || class.Spec.CNPG.Backup == nil || class.Spec.CNPG.Backup.VolumeSnapshot == nil) {
		errs = append(errs, ConfigValidationError{
			Field:   "spec.backup.enabled",
			Value:   true,
			Message: "backup requires cnpg.backup.volumeSnapshot configuration in PostgresClusterClass",
		})
	}

	return errs
}

func parseVersion(version string) (major, minor int) {
	for i, ch := range version {
		if ch == '.' {
			major, _ = strconv.Atoi(version[:i])
			minor, _ = strconv.Atoi(version[i+1:])
			return major, minor
		}
	}
	major, _ = strconv.Atoi(version)
	return major, -1
}

// ValidateMergedConfig checks the merged configuration for required fields and cross-field constraints.
func ValidateMergedConfig(merged *MergedConfig, className string) []ConfigValidationError {
	var errs []ConfigValidationError

	if merged.Spec.Instances == nil {
		errs = append(errs, ConfigValidationError{Field: "spec.instances", Message: "must be set in PostgresCluster or PostgresClusterClass"})
	}
	if merged.Spec.PostgresVersion == nil {
		errs = append(errs, ConfigValidationError{Field: "spec.postgresVersion", Message: "must be set in PostgresCluster or PostgresClusterClass"})
	}
	if merged.Spec.Storage == nil {
		errs = append(errs, ConfigValidationError{Field: "spec.storage", Message: "must be set in PostgresCluster or PostgresClusterClass"})
	}
	if merged.Spec.Backup != nil && merged.Spec.Backup.Enabled != nil && *merged.Spec.Backup.Enabled {
		if merged.Spec.Backup.Schedule == nil || *merged.Spec.Backup.Schedule == "" {
			errs = append(errs, ConfigValidationError{Field: "spec.backup.schedule", Message: "backup.schedule is required when backup.enabled is true"})
		} else if len(strings.Fields(*merged.Spec.Backup.Schedule)) != 5 {
			errs = append(errs, ConfigValidationError{Field: "spec.backup.schedule", Message: "backup.schedule must be a 5-field cron expression (minute hour day month weekday)"})
		}
	}
	if err := validatePostgreSQLConfigNoCNPGFixedKeys(merged.Spec.PostgreSQLConfig); err != nil {
		errs = append(errs, ConfigValidationError{Field: "spec.postgresqlConfig", Message: err.Error()})
	}

	return errs
}

// validatePostgreSQLConfigNoCNPGFixedKeys rejects postgresqlConfig keys that CloudNativePG
// registers as fixed/blocked (see cnpgpostgres.FixedConfigurationParameters). Users must
// not set these; CNPG and the instance manager own them.
func validatePostgreSQLConfigNoCNPGFixedKeys(params map[string]string) error {
	if len(params) == 0 {
		return nil
	}
	invalid := make([]string, 0)
	for k := range params {
		if _, fixed := cnpgpostgres.FixedConfigurationParameters[k]; fixed {
			invalid = append(invalid, k)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	sort.Strings(invalid)
	return fmt.Errorf("postgresqlConfig must not set CNPG-managed parameters: %s", strings.Join(invalid, ", "))
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec.
// IMPORTANT: any field derived from user-controlled CRD fields must also appear in normalizeCNPGClusterSpec,
// otherwise external changes to those fields on the CNPG cluster will be silently ignored.
// Operator-controlled invariants (e.g. SuperuserSecret, EnableSuperuserAccess) are exempt — they
// are always the same value and are never exposed in the PostgresCluster CRD.
func buildCNPGClusterSpec(cfg *MergedConfig, secretName string, postgresMetricsEnabled bool) cnpgv1.ClusterSpec {
	spec := cnpgv1.ClusterSpec{
		ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *cfg.Spec.PostgresVersion),
		Instances: int(*cfg.Spec.Instances),
		PostgresConfiguration: cnpgv1.PostgresConfiguration{
			Parameters: maps.Clone(cfg.Spec.PostgreSQLConfig),
			PgHBA:      cfg.Spec.PgHBA,
		},
		SuperuserSecret:       &cnpgv1.LocalObjectReference{Name: secretName},
		EnableSuperuserAccess: ptr.To(true),
		Bootstrap: &cnpgv1.BootstrapConfiguration{
			InitDB: &cnpgv1.BootstrapInitDB{
				Database: defaultDatabaseName,
				Owner:    superUsername,
				Secret:   &cnpgv1.LocalObjectReference{Name: secretName},
			},
		},
		StorageConfiguration: cnpgv1.StorageConfiguration{
			Size: cfg.Spec.Storage.String(),
		},
		Resources: *cfg.Spec.Resources,
	}
	if cfg.CNPG != nil && cfg.CNPG.PrimaryUpdateMethod != nil {
		spec.PrimaryUpdateMethod = cnpgv1.PrimaryUpdateMethod(*cfg.CNPG.PrimaryUpdateMethod)
	} else {
		spec.PrimaryUpdateMethod = cnpgv1.PrimaryUpdateMethodRestart
	}

	annotations := make(map[string]string)
	if postgresMetricsEnabled {
		annotations = buildPostgresScrapeAnnotations()
	}
	spec.InheritedMetadata = &cnpgv1.EmbeddedObjectMetadata{Annotations: annotations}
	if cfg.Spec.Backup != nil && cfg.Spec.Backup.Enabled != nil && *cfg.Spec.Backup.Enabled && cfg.CNPG != nil && cfg.CNPG.Backup != nil && cfg.CNPG.Backup.VolumeSnapshot != nil {
		spec.Backup = buildCNPGBackupConfiguration(cfg)
	}
	return spec
}

func buildCNPGBackupConfiguration(cfg *MergedConfig) *cnpgv1.BackupConfiguration {
	backupCfg := &cnpgv1.BackupConfiguration{}
	if cfg.CNPG.Backup.Target != nil {
		backupCfg.Target = cnpgv1.BackupTarget(*cfg.CNPG.Backup.Target)
	}
	if vs := cfg.CNPG.Backup.VolumeSnapshot; vs != nil {
		backupCfg.VolumeSnapshot = buildVolumeSnapshotConfiguration(vs)
	}
	return backupCfg
}

func buildVolumeSnapshotConfiguration(vs *enterprisev4.CNPGVolumeSnapshotConfig) *cnpgv1.VolumeSnapshotConfiguration {
	vsCfg := &cnpgv1.VolumeSnapshotConfiguration{}
	if vs.ClassName != nil {
		vsCfg.ClassName = *vs.ClassName
	}
	if vs.WalClassName != nil {
		vsCfg.WalClassName = *vs.WalClassName
	}
	if vs.SnapshotOwnerReference != nil {
		vsCfg.SnapshotOwnerReference = cnpgv1.SnapshotOwnerReference(*vs.SnapshotOwnerReference)
	}
	vsCfg.Online = vs.Online
	vsCfg.Labels = vs.Labels
	vsCfg.Annotations = vs.Annotations
	return vsCfg
}

func buildCNPGCluster(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, secretName string, postgresMetricsEnabled bool) (*cnpgv1.Cluster, error) {
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cfg, secretName, postgresMetricsEnabled),
	}
	if err := ctrl.SetControllerReference(cluster, cnpg, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on CNPG cluster: %w", err)
	}
	return cnpg, nil
}

// stripImageRefForDrift trims whitespace and strips an OCI digest suffix (@sha256:… / @…)
// so drift detection matches tag-only desired images against apiserver materialized refs.
func stripImageRefForDrift(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.Index(name, "@"); i >= 0 {
		return name[:i]
	}
	return name
}

func normalizeCNPGClusterSpec(spec cnpgv1.ClusterSpec, customDefinedParameters map[string]string) normalizedCNPGClusterSpec {
	normalized := normalizedCNPGClusterSpec{
		ImageName:           stripImageRefForDrift(spec.ImageName),
		Instances:           spec.Instances,
		PrimaryUpdateMethod: string(spec.PrimaryUpdateMethod),
		StorageSize:         spec.StorageConfiguration.Size,
		Resources:           spec.Resources,
	}
	if len(customDefinedParameters) > 0 {
		normalized.CustomDefinedParameters = make(map[string]string)
		for k := range customDefinedParameters {
			normalized.CustomDefinedParameters[k] = spec.PostgresConfiguration.Parameters[k]
		}
	}
	if len(spec.PostgresConfiguration.PgHBA) > 0 {
		normalized.PgHBA = spec.PostgresConfiguration.PgHBA
	}
	if spec.InheritedMetadata != nil && len(spec.InheritedMetadata.Annotations) > 0 {
		normalized.InheritedAnnotations = spec.InheritedMetadata.Annotations
	}
	if spec.Bootstrap != nil && spec.Bootstrap.InitDB != nil {
		normalized.DefaultDatabase = spec.Bootstrap.InitDB.Database
		normalized.Owner = spec.Bootstrap.InitDB.Owner
	}
	if spec.Backup != nil {
		normalized.Backup = &normalizedBackupSpec{
			Target: string(spec.Backup.Target),
		}
		if spec.Backup.VolumeSnapshot != nil {
			normalized.Backup.VolumeSnapshotClass = spec.Backup.VolumeSnapshot.ClassName
			normalized.Backup.WalClassName = spec.Backup.VolumeSnapshot.WalClassName
			normalized.Backup.SnapshotOwnerReference = string(spec.Backup.VolumeSnapshot.SnapshotOwnerReference)
			normalized.Backup.Online = spec.Backup.VolumeSnapshot.Online
			normalized.Backup.Labels = spec.Backup.VolumeSnapshot.Labels
			normalized.Backup.Annotations = spec.Backup.VolumeSnapshot.Annotations
		}
	}
	return normalized
}

// cnpgPatchKind classifies Actuate's drift outcome so Converge can gate
// ClusterReady on CNPG.Status.Phase only for material changes — annotation
// drift propagates via metadata PATCH without a phase transition. See isClusterDrift.
type cnpgPatchKind int

const (
	cnpgPatchNone     cnpgPatchKind = iota // no drift detected; nothing patched.
	cnpgPatchMetadata                      // InheritedAnnotations changed only; metadata-only.
	cnpgPatchBody                          // structural change; CNPG must observably reconcile.
)

// requiresPhaseGate reports whether Converge should hold ClusterReady=Provisioning
// while CNPG.Status.Phase still reflects the pre-patch value. Annotation-only
// patches do not need this gate (see isClusterDrift).
func (k cnpgPatchKind) requiresPhaseGate() bool { return k == cnpgPatchBody }

// isClusterDrift reports whether two normalized specs differ in any field CNPG
// must observably reconcile against. Every field is material except
// InheritedAnnotations (metadata-only; gating it would deadlock). Pass-by-value
// is deliberate so nil-ing locals does not mutate the caller's specs.
func isClusterDrift(a, b normalizedCNPGClusterSpec) bool {
	a.InheritedAnnotations = nil
	b.InheritedAnnotations = nil
	return !equality.Semantic.DeepEqual(a, b)
}

// reconcileManagedRoles synchronizes ManagedRoles from PostgresCluster spec to CNPG Cluster managed.roles.
func reconcileManagedRoles(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := logging.FromContext(ctx).With("func", "reconcileManagedRoles")

	if len(cluster.Spec.ManagedRoles) == 0 {
		logger.InfoContext(ctx, "no managed roles to reconcile")
		return nil
	}

	desiredRoles := make([]cnpgv1.RoleConfiguration, 0, len(cluster.Spec.ManagedRoles))
	for _, role := range cluster.Spec.ManagedRoles {
		r := cnpgv1.RoleConfiguration{
			Name:   role.Name,
			Ensure: cnpgv1.EnsureAbsent,
		}
		if role.Exists {
			r.Ensure = cnpgv1.EnsurePresent
			r.Login = true
		}
		if role.PasswordSecretRef != nil {
			// Pass only the secret name to CNPG — CNPG always reads the "password" key.
			r.PasswordSecret = &cnpgv1.LocalObjectReference{Name: role.PasswordSecretRef.LocalObjectReference.Name}
		}
		desiredRoles = append(desiredRoles, r)
	}

	var currentRoles []cnpgv1.RoleConfiguration
	if cnpgCluster.Spec.Managed != nil {
		currentRoles = cnpgCluster.Spec.Managed.Roles
	}

	if equality.Semantic.DeepEqual(currentRoles, desiredRoles) {
		logger.InfoContext(ctx, "CNPG Cluster roles already match desired state, no update needed")
		return nil
	}

	logger.InfoContext(ctx, "CNPG Cluster roles drift detected, update started",
		"currentCount", len(currentRoles), "desiredCount", len(desiredRoles))

	originalCluster := cnpgCluster.DeepCopy()
	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desiredRoles

	if err := c.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); err != nil {
		return fmt.Errorf("patching CNPG Cluster managed roles: %w", err)
	}
	logger.InfoContext(ctx, "CNPG Cluster managed roles updated", "roleCount", len(desiredRoles))
	return nil
}

func poolerResourceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s%s%s", clusterName, defaultPoolerSuffix, poolerType)
}

func poolerExists(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, poolerType string) (bool, error) {
	pooler := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(cluster.Name, poolerType),
		Namespace: cluster.Namespace,
	}, pooler)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func arePoolersReady(rwPooler, roPooler *cnpgv1.Pooler) bool {
	return isPoolerReady(rwPooler) && isPoolerReady(roPooler)
}

// isPoolerReady checks if a pooler has all instances scheduled.
// CNPG PoolerStatus only tracks scheduled instances, not ready pods.
func isPoolerReady(pooler *cnpgv1.Pooler) bool {
	desired := int32(1)
	if pooler.Spec.Instances != nil {
		desired = *pooler.Spec.Instances
	}
	return pooler.Status.Instances >= desired
}

// createOrUpdateConnectionPoolers creates RW and RO poolers if they don't exist.
func createOrUpdateConnectionPoolers(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerMetricsEnabled bool) error {
	if err := createConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readWriteEndpoint, poolerMetricsEnabled); err != nil {
		return fmt.Errorf("reconciling RW pooler: %w", err)
	}
	if err := createConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readOnlyEndpoint, poolerMetricsEnabled); err != nil {
		return fmt.Errorf("reconciling RO pooler: %w", err)
	}
	return nil
}

func createConnectionPooler(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string, poolerMetricsEnabled bool) error {
	logger := logging.FromContext(ctx).With("func", "createConnectionPooler")
	poolerName := poolerResourceName(cluster.Name, poolerType)
	existing := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	logger.InfoContext(ctx, "CNPG Pooler creation started", "name", poolerName, "type", poolerType)
	pooler, err := buildCNPGPooler(scheme, cluster, cfg, cnpgCluster, poolerType, poolerMetricsEnabled)
	if err != nil {
		return err
	}
	return c.Create(ctx, pooler)
}

func buildCNPGPooler(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string, poolerMetricsEnabled bool) (*cnpgv1.Pooler, error) {
	pc := cfg.CNPG.ConnectionPooler
	instances := *pc.Instances
	mode := cnpgv1.PgBouncerPoolMode(*pc.Mode)
	pooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, poolerType), Namespace: cluster.Namespace},
		Spec: cnpgv1.PoolerSpec{
			Cluster:   cnpgv1.LocalObjectReference{Name: cnpgCluster.Name},
			Instances: &instances,
			Type:      cnpgv1.PoolerType(poolerType),
			PgBouncer: &cnpgv1.PgBouncerSpec{
				PoolMode:   mode,
				Parameters: pc.Config,
			},
		},
	}
	poolerAnnotations := make(map[string]string)
	if poolerMetricsEnabled {
		poolerAnnotations = buildPoolerScrapeAnnotations()
	}
	// Template always set so merge patches can express annotation removal.
	// CNPG's Pooler CRD requires template.spec.containers; a stub "pgbouncer"
	// container lets CNPG fill in image/command/ports.
	pooler.Spec.Template = &cnpgv1.PodTemplateSpec{
		ObjectMeta: cnpgv1.Metadata{Annotations: poolerAnnotations},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "pgbouncer"}},
		},
	}
	if err := ctrl.SetControllerReference(cluster, pooler, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on CNPG pooler: %w", err)
	}
	return pooler, nil
}

// deleteConnectionPoolers removes RW and RO poolers if they exist.
func deleteConnectionPoolers(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) error {
	logger := logging.FromContext(ctx).With("func", "deleteConnectionPoolers")
	for _, poolerType := range []string{readWriteEndpoint, readOnlyEndpoint} {
		poolerName := poolerResourceName(cluster.Name, poolerType)
		exist, err := poolerExists(ctx, c, cluster, poolerType)
		if err != nil {
			return fmt.Errorf("checking pooler existence: %w", err)
		}
		if !exist {
			continue
		}
		pooler := &cnpgv1.Pooler{}
		if err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, pooler); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting pooler %s: %w", poolerName, err)
		}
		logger.InfoContext(ctx, "CNPG Pooler deletion started", "name", poolerName)
		if err := c.Delete(ctx, pooler); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting pooler %s: %w", poolerName, err)
		}
	}
	return nil
}

// setStatus sets the phase, condition and persists the status.
// It skips the API write when the resulting status is identical to the current
// state, avoiding unnecessary etcd churn and ResourceVersion bumps on stable clusters.
func setStatus(ctx context.Context, c client.Client, metrics ports.Recorder, cluster *enterprisev4.PostgresCluster, condType conditionTypes, status metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileClusterPhases) error {
	before := cluster.Status.DeepCopy()

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

func setStatusFromHealth(ctx context.Context, c client.Client, metrics ports.Recorder, cluster *enterprisev4.PostgresCluster, health componentHealth) error {
	conditionStatus := metav1.ConditionFalse
	if health.State == pgcConstants.Ready {
		conditionStatus = metav1.ConditionTrue
	}
	return setStatus(ctx, c, metrics, cluster, health.Condition, conditionStatus, health.Reason, health.Message, health.Phase)
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

// caMetadataForConfigMap returns a SecretKeySelector for the CNPG server CA Secret when it is
// published and contains the expected data key (same SecretKeySelector shape as database secret refs).
func caMetadataForConfigMap(
	ctx context.Context,
	c client.Client,
	namespace string,
	cnpgCluster *cnpgv1.Cluster,
) (*corev1.SecretKeySelector, bool, error) {
	name := cnpgCluster.Status.Certificates.ServerCASecret
	if name == "" {
		return nil, false, nil // not ready yet — omit keys
	}
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil // status ahead of materialization — omit keys, requeue later
		}
		return nil, false, err
	}
	key := defaultServerCACertKey
	if len(sec.Data[key]) == 0 && len(sec.StringData[key]) == 0 {
		// Secret exists but unexpected shape — don't advertise a broken contract
		return nil, false, nil
	}
	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}, true, nil
}

// generateConfigMap builds a ConfigMap with connection details for the
// PostgresCluster. tlsLeafAligned gates the CLUSTER_POOLER_* keys.
func generateConfigMap(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, secretName string, tlsLeafAligned bool) (*corev1.ConfigMap, error) {
	cmName := fmt.Sprintf("%s%s", cluster.Name, defaultConfigMapSuffix)
	if cluster.Status.Resources != nil && cluster.Status.Resources.ConfigMapRef != nil {
		cmName = cluster.Status.Resources.ConfigMapRef.Name
	}

	data := map[string]string{
		configKeyClusterRWEndpoint:  fmt.Sprintf("%s-rw.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		configKeyClusterROEndpoint:  fmt.Sprintf("%s-ro.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		configKeyClusterREndpoint:   fmt.Sprintf("%s-r.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		configKeyDefaultClusterPort: defaultPort,
		configKeySuperUserName:      superUsername,
		configKeySuperUserSecretRef: secretName,
	}

	caSecretRef, ok, err := caMetadataForConfigMap(ctx, c, cluster.Namespace, cnpgCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get CA metadata for ConfigMap: %w", err)
	}
	if ok {
		data[configKeyServerCASecretRef] = fmt.Sprintf("%s/%s", caSecretRef.Name, caSecretRef.Key)
	}

	rwExists, err := poolerExists(ctx, c, cluster, readWriteEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to check RW pooler existence: %w", err)
	}
	roExists, err := poolerExists(ctx, c, cluster, readOnlyEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to check RO pooler existence: %w", err)
	}
	// Gate on tlsLeafAligned so sslmode=verify-full consumers don't dial the
	// pooler hostname before the leaf carries its SAN.
	if rwExists && roExists && tlsLeafAligned {
		data[configKeyPoolerRWEndpoint] = fmt.Sprintf("%s.%s", poolerResourceName(cnpgCluster.Name, readWriteEndpoint), cnpgCluster.Namespace)
		data[configKeyPoolerROEndpoint] = fmt.Sprintf("%s.%s", poolerResourceName(cnpgCluster.Name, readOnlyEndpoint), cnpgCluster.Namespace)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cluster.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "postgrescluster-controller"},
		},
		Data: data,
	}
	if err := ctrl.SetControllerReference(cluster, cm, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}
	return cm, nil
}

// ensureClusterSecret creates the superuser secret (caller must verify it is missing) and persists the ref to status.
func ensureClusterSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, secretName string) error {
	pw, err := generatePassword()
	if err != nil {
		return err
	}
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: cluster.Namespace},
		StringData: map[string]string{"username": superUsername, "password": pw},
		Type:       corev1.SecretTypeOpaque,
	}
	if err := ctrl.SetControllerReference(cluster, newSecret, scheme); err != nil {
		return err
	}
	if err := c.Create(ctx, newSecret); err != nil {
		return err
	}
	if cluster.Status.Resources == nil {
		cluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}
	cluster.Status.Resources.SuperUserSecretRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		Key:                  secretKeyPassword,
	}
	return nil
}

func clusterSecretExists(ctx context.Context, c client.Client, namespace, name string, secret *corev1.Secret) (bool, error) {
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
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
func handleFinalizer(ctx context.Context, rc *ReconcileContext, cluster *enterprisev4.PostgresCluster, secret *corev1.Secret) error {
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
			if err := patchObject(ctx, c, originalCNPG, cnpgCluster, "CNPGCluster"); err != nil {
				return fmt.Errorf("patching CNPG cluster after removing owner reference: %w", err)
			}
			logger.InfoContext(ctx, "removed owner reference from CNPG Cluster")
		}

		// Remove owner reference from the superuser Secret to prevent cascading deletion.
		if cluster.Status.Resources != nil && cluster.Status.Resources.SuperUserSecretRef != nil {
			secretName := cluster.Status.Resources.SuperUserSecretRef.Name
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

func generatePassword() (string, error) {
	const (
		length  = 32
		digits  = 8
		symbols = 0
	)
	return password.Generate(length, digits, symbols, false, true)
}
