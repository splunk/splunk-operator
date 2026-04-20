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
	"sort"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	password "github.com/sethvargo/go-password/password"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	log "sigs.k8s.io/controller-runtime/pkg/log"
)

// PostgresClusterService is the application service entry point called by the primary adapter (reconciler).
func PostgresClusterService(ctx context.Context, rc *ReconcileContext, req ctrl.Request) (ctrl.Result, error) {
	c := rc.Client
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgresCluster")

	var cnpgCluster *cnpgv1.Cluster
	var poolerEnabled bool
	var postgresSecretName string
	secret := &corev1.Secret{}

	// 1. Fetch the PostgresCluster instance, stop if not found.
	postgresCluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, req.NamespacedName, postgresCluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PostgresCluster deleted, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to fetch PostgresCluster")
		return ctrl.Result{}, err
	}
	if postgresCluster.Status.Resources == nil {
		postgresCluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}

	logger = logger.WithValues("postgresCluster", postgresCluster.Name)
	ctx = log.IntoContext(ctx, logger)

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
			logger.Info("PostgresCluster already deleted, skipping finalizer update")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to handle finalizer")
		rc.emitWarning(postgresCluster, EventCleanupFailed, fmt.Sprintf("Cleanup failed: %v", err))
		statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterDeleteFailed,
			fmt.Sprintf("Failed to delete resources during cleanup: %v", err), failedClusterPhase)
		return ctrl.Result{}, errors.Join(err, statusErr)
	}
	if postgresCluster.GetDeletionTimestamp() != nil {
		logger.Info("Deletion cleanup complete, finalizer removed")
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(postgresCluster, PostgresClusterFinalizerName) {
		controllerutil.AddFinalizer(postgresCluster, PostgresClusterFinalizerName)
		if err := c.Update(ctx, postgresCluster); err != nil {
			logger.Error(err, "Failed to add finalizer to PostgresCluster")
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		logger.Info("Finalizer added")
		return ctrl.Result{}, nil
	}

	// Load the referenced PostgresClusterClass.
	clusterClass := &enterprisev4.PostgresClusterClass{}
	if err := c.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, clusterClass); err != nil {
		logger.Error(err, "Failed to fetch PostgresClusterClass", "className", postgresCluster.Spec.Class)
		rc.emitWarning(postgresCluster, EventClusterClassNotFound, fmt.Sprintf("ClusterClass %s not found", postgresCluster.Spec.Class))
		statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterClassNotFound,
			fmt.Sprintf("ClusterClass %s not found: %v", postgresCluster.Spec.Class, err), failedClusterPhase)
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	// Merge PostgresClusterSpec on top of PostgresClusterClass defaults.
	mergedConfig, err := getMergedConfig(clusterClass, postgresCluster)
	if err != nil {
		logger.Error(err, "Failed to merge PostgresCluster configuration")
		rc.emitWarning(postgresCluster, EventConfigMergeFailed, fmt.Sprintf("Failed to merge configuration: %v", err))
		statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonInvalidConfiguration,
			fmt.Sprintf("Failed to merge configuration: %v", err), failedClusterPhase)
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	// Resolve or derive the superuser secret name.
	if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.SuperUserSecretRef != nil {
		postgresSecretName = postgresCluster.Status.Resources.SuperUserSecretRef.Name
		logger.Info("Superuser secret resolved from status", "name", postgresSecretName)
	} else {
		postgresSecretName = fmt.Sprintf("%s%s", postgresCluster.Name, defaultSecretSuffix)
		logger.Info("Superuser secret name derived", "name", postgresSecretName)
	}

	poolerEnabled = mergedConfig.Spec.ConnectionPoolerEnabled != nil && *mergedConfig.Spec.ConnectionPoolerEnabled
	poolerConfigPresent := mergedConfig.CNPG != nil && mergedConfig.CNPG.ConnectionPooler != nil

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
			newPoolerModel(c, rc.Scheme, rc, updateComponentHealthStatus, postgresCluster, clusterClass, mergedConfig, cnpgCluster, poolerEnabled, poolerConfigPresent),
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

	logger.Info("Reconciliation complete")
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
	logger     logr.Logger
}

func (m *componentManager) Handle(ctx context.Context) (ctrl.Result, error) {
	for _, component := range m.components {
		componentLogger := m.logger.WithValues("component", component.Name())
		gate := component.EvaluatePrerequisites(ctx)

		if gate.Allowed {
			component.Actuate(ctx)
		} else {
			componentLogger.Info("Component blocked by prerequisites",
				"step", "prerequisites",
				"condition", gate.Health.Condition,
				"reason", gate.Health.Reason,
				"phase", gate.Health.Phase,
				"requeueAfter", gate.Health.Result.RequeueAfter)
		}

		health, err := component.Converge(ctx)
		if err != nil && isTransientError(err) {
			componentLogger.Error(err, "Component convergence transient error, requeueing", "step", "converge")
			return transientResult(err), nil
		}

		if err != nil {
			componentLogger.Error(err, "Component convergence failed",
				"step", "converge",
				"condition", health.Condition,
				"reason", health.Reason,
				"phase", health.Phase)
			return health.Result, fmt.Errorf("%s converge: %w", component.Name(), err)
		}
		if isIntermediateState(health.State) {
			componentLogger.Info("Component convergence pending",
				"step", "converge",
				"condition", health.Condition,
				"reason", health.Reason,
				"phase", health.Phase,
				"requeueAfter", health.Result.RequeueAfter)
			return health.Result, nil
		}
		componentLogger.Info("Component convergence ready",
			"step", "converge",
			"condition", health.Condition,
			"reason", health.Reason,
			"phase", health.Phase)
		if health.Result != (ctrl.Result{}) {
			componentLogger.Info("Component requested explicit result",
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
	cnpgPatched  bool

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
	p.cnpgPatched = false

	desiredSpec := buildCNPGClusterSpec(p.mergedConfig, p.secretName, p.metricsEnabled)
	existingCNPG := &cnpgv1.Cluster{}
	err := p.client.Get(ctx, types.NamespacedName{Name: p.cluster.Name, Namespace: p.cluster.Namespace}, existingCNPG)
	switch {
	case apierrors.IsNotFound(err):
		newCluster, err := buildCNPGCluster(p.scheme, p.cluster, p.mergedConfig, p.secretName, p.metricsEnabled)
		if err != nil {
			p.events.emitWarning(p.cluster, EventClusterCreateFailed, fmt.Sprintf("Failed to build CNPG cluster: %v", err))
			p.health.State = pgcConstants.Failed
			p.health.Reason = reasonClusterBuildFailed
			p.health.Message = fmt.Sprintf("Failed to build CNPG cluster: %v", err)
			p.health.Phase = failedClusterPhase
			p.health.Result = ctrl.Result{}
			p.actuateErr = err
			return
		}
		if err = p.client.Create(ctx, newCluster); err != nil {
			p.events.emitWarning(p.cluster, EventClusterCreateFailed, fmt.Sprintf("Failed to create CNPG cluster: %v", err))
			p.health.State = pgcConstants.Failed
			p.health.Reason = reasonClusterBuildFailed
			p.health.Message = fmt.Sprintf("Failed to create CNPG cluster: %v", err)
			p.health.Phase = failedClusterPhase
			p.health.Result = ctrl.Result{}
			p.actuateErr = err
			return
		}
		p.events.emitNormal(p.cluster, EventClusterCreationStarted, "CNPG cluster created, waiting for healthy state")
		p.cnpgCluster = newCluster
		p.cnpgCreated = true
	case err != nil:
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonClusterGetFailed
		p.health.Message = fmt.Sprintf("Failed to get CNPG cluster: %v", err)
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		p.actuateErr = err
		return
	default:
		p.cnpgCluster = existingCNPG
		currentNormalized := normalizeCNPGClusterSpec(p.cnpgCluster.Spec, p.mergedConfig.Spec.PostgreSQLConfig)
		desiredNormalized := normalizeCNPGClusterSpec(desiredSpec, p.mergedConfig.Spec.PostgreSQLConfig)
		if !equality.Semantic.DeepEqual(currentNormalized, desiredNormalized) {
			originalCluster := p.cnpgCluster.DeepCopy()
			p.cnpgCluster.Spec = desiredSpec
			if patchErr := patchObject(ctx, p.client, originalCluster, p.cnpgCluster, "CNPGCluster"); patchErr != nil {
				p.events.emitWarning(p.cluster, EventClusterUpdateFailed, fmt.Sprintf("Failed to patch CNPG cluster: %v", patchErr))
				p.health.State = pgcConstants.Failed
				p.health.Reason = reasonClusterPatchFailed
				p.health.Message = fmt.Sprintf("Failed to patch CNPG cluster: %v", patchErr)
				p.health.Phase = failedClusterPhase
				p.health.Result = ctrl.Result{}
				p.actuateErr = patchErr
				return
			}
			p.events.emitNormal(p.cluster, EventClusterUpdateStarted, "CNPG cluster spec updated, waiting for healthy state")
			p.cnpgPatched = true
		}
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
	return
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

	if p.cnpgCluster == nil {
		p.health.State = pgcConstants.Pending
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = msgCNPGPendingCreation
		p.health.Phase = pendingClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}

	if p.cnpgCreated {
		p.health.State = pgcConstants.Pending
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = msgCNPGPendingCreation
		p.health.Phase = pendingClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}

	if p.cnpgPatched {
		p.health.State = pgcConstants.Provisioning
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = fmt.Sprintf(msgFmtCNPGClusterPhase, p.cnpgCluster.Status.Phase)
		p.health.Phase = provisioningClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}

	switch p.cnpgCluster.Status.Phase {
	case cnpgv1.PhaseHealthy:
		p.health.State = pgcConstants.Ready
		p.health.Reason = reasonCNPGClusterHealthy
		p.health.Message = msgProvisionerHealthy
		p.health.Phase = readyClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, nil
	case cnpgv1.PhaseFirstPrimary, cnpgv1.PhaseCreatingReplica, cnpgv1.PhaseWaitingForInstancesToBeActive:
		p.health.State = pgcConstants.Provisioning
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = fmt.Sprintf(msgFmtCNPGProvisioning, p.cnpgCluster.Status.Phase)
		p.health.Phase = provisioningClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	case cnpgv1.PhaseSwitchover:
		p.health.State = pgcConstants.Configuring
		p.health.Reason = reasonCNPGSwitchover
		p.health.Message = msgCNPGSwitchover
		p.health.Phase = configuringClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	case cnpgv1.PhaseFailOver:
		p.health.State = pgcConstants.Configuring
		p.health.Reason = reasonCNPGFailingOver
		p.health.Message = msgCNPGFailingOver
		p.health.Phase = configuringClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	case cnpgv1.PhaseInplacePrimaryRestart, cnpgv1.PhaseInplaceDeletePrimaryRestart:
		p.health.State = pgcConstants.Configuring
		p.health.Reason = reasonCNPGRestarting
		p.health.Message = fmt.Sprintf(msgFmtCNPGRestarting, p.cnpgCluster.Status.Phase)
		p.health.Phase = configuringClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	case cnpgv1.PhaseUpgrade, cnpgv1.PhaseMajorUpgrade, cnpgv1.PhaseUpgradeDelayed, cnpgv1.PhaseOnlineUpgrading:
		p.health.State = pgcConstants.Configuring
		p.health.Reason = reasonCNPGUpgrading
		p.health.Message = fmt.Sprintf(msgFmtCNPGUpgrading, p.cnpgCluster.Status.Phase)
		p.health.Phase = configuringClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	case cnpgv1.PhaseApplyingConfiguration:
		p.health.State = pgcConstants.Configuring
		p.health.Reason = reasonCNPGApplyingConfig
		p.health.Message = msgCNPGApplyingConfiguration
		p.health.Phase = configuringClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	case cnpgv1.PhaseReplicaClusterPromotion:
		p.health.State = pgcConstants.Configuring
		p.health.Reason = reasonCNPGPromoting
		p.health.Message = msgCNPGPromoting
		p.health.Phase = configuringClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	case cnpgv1.PhaseWaitingForUser:
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonCNPGWaitingForUser
		p.health.Message = msgCNPGWaitingForUser
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, fmt.Errorf("provisioner requires user action")
	case cnpgv1.PhaseUnrecoverable:
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonCNPGUnrecoverable
		p.health.Message = msgCNPGUnrecoverable
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, fmt.Errorf("provisioner unrecoverable")
	case cnpgv1.PhaseCannotCreateClusterObjects:
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonCNPGProvisioningFailed
		p.health.Message = msgCNPGCannotCreateObjects
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, fmt.Errorf("provisioner cannot create cluster objects")
	case cnpgv1.PhaseUnknownPlugin, cnpgv1.PhaseFailurePlugin:
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonCNPGPluginError
		p.health.Message = fmt.Sprintf(msgFmtCNPGPluginError, p.cnpgCluster.Status.Phase)
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, fmt.Errorf("provisioner plugin error")
	case cnpgv1.PhaseImageCatalogError, cnpgv1.PhaseArchitectureBinaryMissing:
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonCNPGImageError
		p.health.Message = fmt.Sprintf(msgFmtCNPGImageError, p.cnpgCluster.Status.Phase)
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, fmt.Errorf("provisioner image error")
	case "":
		p.health.State = pgcConstants.Pending
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = msgCNPGPendingCreation
		p.health.Phase = pendingClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	default:
		p.health.State = pgcConstants.Provisioning
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = fmt.Sprintf(msgFmtCNPGClusterPhase, p.cnpgCluster.Status.Phase)
		p.health.Phase = provisioningClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}
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
	if rolesErr := reconcileManagedRoles(ctx, m.client, m.cluster, m.runtime.Cluster()); rolesErr != nil {
		m.events.emitWarning(m.cluster, EventManagedRolesFailed, fmt.Sprintf("Failed to reconcile managed roles: %v", rolesErr))
		m.health.State = pgcConstants.Failed
		m.health.Reason = reasonManagedRolesFailed
		m.health.Message = fmt.Sprintf("Failed to reconcile managed roles: %v", rolesErr)
		m.health.Phase = failedClusterPhase
		m.health.Result = ctrl.Result{}
		m.actuateErr = rolesErr
		return
	}
	return
}

func (m *managedRolesModel) Converge(ctx context.Context) (health componentHealth, err error) {
	_ = ctx
	m.health.Condition = managedRolesReady
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
		m.health.State = pgcConstants.Failed
		m.health.Reason = reasonManagedRolesFailed
		m.health.Message = "Managed roles status not published yet"
		m.health.Phase = failedClusterPhase
		m.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		m.emitManagedRolesConvergeFailure(m.health.Message)
		return m.health, fmt.Errorf("managed roles status not published")
	}

	if len(status.Failed) > 0 {
		m.health.State = pgcConstants.Failed
		m.health.Reason = reasonManagedRolesFailed
		m.health.Message = fmt.Sprintf("Managed roles reconciliation failed for %d role(s)", len(status.Failed))
		m.health.Phase = failedClusterPhase
		m.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		m.emitManagedRolesConvergeFailure(m.health.Message)
		return m.health, fmt.Errorf("managed roles have failed entries")
	}

	if len(status.Pending) > 0 {
		m.health.State = pgcConstants.Pending
		m.health.Reason = reasonManagedRolesPending
		m.health.Message = fmt.Sprintf("Managed roles pending for %d role(s)", len(status.Pending))
		m.health.Phase = pendingClusterPhase
		m.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return m.health, nil
	}

	m.health.State = pgcConstants.Ready
	m.health.Reason = reasonManagedRolesReady
	m.health.Message = "Managed roles are reconciled"
	m.health.Phase = readyClusterPhase
	m.health.Result = ctrl.Result{}
	if !meta.IsStatusConditionTrue(m.cluster.Status.Conditions, string(managedRolesReady)) {
		m.events.emitNormal(m.cluster, EventManagedRolesReady, m.health.Message)
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
}

func newPoolerModel(c client.Client, scheme *runtime.Scheme, events poolerEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, clusterClass *enterprisev4.PostgresClusterClass, mergedConfig *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerEnabled bool, poolerConfigPresent bool) *poolerModel {
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
			p.health.State = pgcConstants.Failed
			p.health.Reason = reasonPoolerReconciliationFailed
			p.health.Message = fmt.Sprintf("Failed to delete poolers: %v", err)
			p.health.Phase = failedClusterPhase
			p.health.Result = ctrl.Result{}
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
		if err := createOrUpdateConnectionPoolers(ctx, p.client, p.scheme, p.cluster, p.mergedConfig, p.cnpgCluster, p.metricsEnabled); err != nil {
			p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, fmt.Sprintf("Failed to reconcile connection pooler: %v", err))
			p.health.State = pgcConstants.Failed
			p.health.Reason = reasonPoolerReconciliationFailed
			p.health.Message = fmt.Sprintf("Failed to reconcile connection pooler: %v", err)
			p.health.Phase = failedClusterPhase
			p.health.Result = ctrl.Result{}
			p.actuateErr = err
			return
		}
		return
	}
}

func (p *poolerModel) Converge(ctx context.Context) (health componentHealth, err error) {
	p.health.Condition = poolerReady
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

	if !p.poolerEnabled {
		p.health.State = pgcConstants.Ready
		p.health.Reason = reasonAllInstancesReady
		p.health.Message = msgPoolerDisabled
		p.health.Phase = readyClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, nil
	}
	if !p.poolerConfigPresent {
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonPoolerConfigMissing
		p.health.Message = msgPoolerConfigMissing
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, fmt.Errorf("pooler config missing")
	}
	if p.actuateErr != nil {
		return p.health, p.actuateErr
	}
	if p.cnpgCluster == nil {
		p.health.State = pgcConstants.Pending
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = msgCNPGPendingCreation
		p.health.Phase = pendingClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}
	if p.cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
		p.health.State = pgcConstants.Provisioning
		p.health.Reason = reasonCNPGProvisioning
		p.health.Message = fmt.Sprintf(msgFmtCNPGClusterPhase, p.cnpgCluster.Status.Phase)
		p.health.Phase = provisioningClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}

	// TODO: Port material.
	rwExists, err := poolerExists(ctx, p.client, p.cluster, readWriteEndpoint)
	if err != nil {
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, fmt.Sprintf("Failed to sync pooler status: %v", err))
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonPoolerReconciliationFailed
		p.health.Message = fmt.Sprintf("Failed to check RW pooler existence: %v", err)
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, err
	}
	roExists, err := poolerExists(ctx, p.client, p.cluster, readOnlyEndpoint)
	if err != nil {
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, fmt.Sprintf("Failed to sync pooler status: %v", err))
		p.health.State = pgcConstants.Failed
		p.health.Reason = reasonPoolerReconciliationFailed
		p.health.Message = fmt.Sprintf("Failed to check RO pooler existence: %v", err)
		p.health.Phase = failedClusterPhase
		p.health.Result = ctrl.Result{}
		return p.health, err
	}
	if !rwExists || !roExists {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health.State = pgcConstants.Provisioning
		p.health.Reason = reasonPoolerCreating
		p.health.Message = msgPoolersProvisioning
		p.health.Phase = provisioningClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}

	rwPooler := &cnpgv1.Pooler{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(p.cluster.Name, readWriteEndpoint),
		Namespace: p.cluster.Namespace,
	}, rwPooler); err != nil {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health.State = pgcConstants.Pending
		p.health.Reason = reasonPoolerCreating
		p.health.Message = msgWaitRWPoolerObject
		p.health.Phase = pendingClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}
	roPooler := &cnpgv1.Pooler{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(p.cluster.Name, readOnlyEndpoint),
		Namespace: p.cluster.Namespace,
	}, roPooler); err != nil {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health.State = pgcConstants.Pending
		p.health.Reason = reasonPoolerCreating
		p.health.Message = msgWaitROPoolerObject
		p.health.Phase = pendingClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}
	if !arePoolersReady(rwPooler, roPooler) {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		p.health.State = pgcConstants.Pending
		p.health.Reason = reasonPoolerCreating
		p.health.Message = msgPoolersNotReady
		p.health.Phase = pendingClusterPhase
		p.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return p.health, nil
	}

	p.cluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{Enabled: true}
	p.health.State = pgcConstants.Ready
	p.health.Reason = reasonAllInstancesReady
	p.health.Message = msgPoolersReady
	p.health.Phase = readyClusterPhase
	p.health.Result = ctrl.Result{}
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
	desiredCM, err := generateConfigMap(ctx, c.client, c.scheme, c.cluster, cnpgCluster, c.secret)
	if err != nil {
		c.events.emitWarning(c.cluster, EventConfigMapReconcileFailed, fmt.Sprintf("Failed to reconcile ConfigMap: %v", err))
		c.health.State = pgcConstants.Failed
		c.health.Reason = reasonConfigMapFailed
		c.health.Message = fmt.Sprintf("Failed to reconcile ConfigMap: %v", err)
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
		c.events.emitWarning(c.cluster, EventConfigMapReconcileFailed, fmt.Sprintf("Failed to reconcile ConfigMap: %v", err))
		c.health.State = pgcConstants.Failed
		c.health.Reason = reasonConfigMapFailed
		c.health.Message = fmt.Sprintf("Failed to reconcile ConfigMap: %v", err)
		c.health.Phase = failedClusterPhase
		c.health.Result = ctrl.Result{}
		c.actuateErr = err
		return
	}
	if op == controllerutil.OperationResultCreated {
		c.events.emitNormal(c.cluster, EventConfigMapReconciled, fmt.Sprintf("ConfigMap %s created", desiredCM.Name))
	} else if op == controllerutil.OperationResultUpdated {
		c.events.emitNormal(c.cluster, EventConfigMapReconciled, fmt.Sprintf("ConfigMap %s updated", desiredCM.Name))
	}
	if c.cluster.Status.Resources.ConfigMapRef == nil {
		c.cluster.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: desiredCM.Name}
	}
	return
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

	c.health.State = pgcConstants.Ready
	c.health.Reason = reasonConfigMapReady
	c.health.Message = msgAccessConfigMapReady
	c.health.Phase = readyClusterPhase
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
		s.events.emitWarning(s.cluster, EventSecretReconcileFailed, fmt.Sprintf("Failed to check secret existence: %v", secretErr))
		s.health.State = pgcConstants.Failed
		s.health.Reason = reasonSuperUserSecretFailed
		s.health.Message = fmt.Sprintf("Failed to check secret existence: %v", secretErr)
		s.health.Phase = failedClusterPhase
		s.health.Result = ctrl.Result{}
		s.actuateErr = secretErr
		return
	}
	if !secretExists {
		if err := ensureClusterSecret(ctx, s.client, s.scheme, s.cluster, s.name, secret); err != nil {
			s.events.emitWarning(s.cluster, EventSecretReconcileFailed, fmt.Sprintf("Failed to generate cluster secret: %v", err))
			s.health.State = pgcConstants.Failed
			s.health.Reason = reasonSuperUserSecretFailed
			s.health.Message = fmt.Sprintf("Failed to generate cluster secret: %v", err)
			s.health.Phase = failedClusterPhase
			s.health.Result = ctrl.Result{}
			s.actuateErr = err
			return
		}
	}
	hasOwnerRef, ownerRefErr := controllerutil.HasOwnerReference(secret.GetOwnerReferences(), s.cluster, s.scheme)
	if ownerRefErr != nil {
		s.health.State = pgcConstants.Failed
		s.health.Reason = reasonSuperUserSecretFailed
		s.health.Message = fmt.Sprintf("failed to check owner reference on secret: %v", ownerRefErr)
		s.health.Phase = failedClusterPhase
		s.health.Result = ctrl.Result{}
		s.actuateErr = fmt.Errorf("failed to check owner reference on secret: %w", ownerRefErr)
		return
	}
	if secretExists && !hasOwnerRef {
		originalSecret := secret.DeepCopy()
		if err := ctrl.SetControllerReference(s.cluster, secret, s.scheme); err != nil {
			s.health.State = pgcConstants.Failed
			s.health.Reason = reasonSuperUserSecretFailed
			s.health.Message = fmt.Sprintf("failed to set controller reference on existing secret: %v", err)
			s.health.Phase = failedClusterPhase
			s.health.Result = ctrl.Result{}
			s.actuateErr = fmt.Errorf("failed to set controller reference on existing secret: %w", err)
			return
		}
		if err := patchObject(ctx, s.client, originalSecret, secret, "Secret"); err != nil {
			s.events.emitWarning(s.cluster, EventSecretReconcileFailed, fmt.Sprintf("Failed to patch existing secret: %v", err))
			s.health.State = pgcConstants.Failed
			s.health.Reason = reasonSuperUserSecretFailed
			s.health.Message = fmt.Sprintf("Failed to patch existing secret: %v", err)
			s.health.Phase = failedClusterPhase
			s.health.Result = ctrl.Result{}
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
	return
}

func (s *secretModel) Converge(ctx context.Context) (health componentHealth, err error) {
	s.health.Condition = secretsReady
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
		s.health.State = pgcConstants.Provisioning
		s.health.Reason = reasonUserSecretPending
		s.health.Message = msgSecretRefNotPublished
		s.health.Phase = provisioningClusterPhase
		s.health.Result = ctrl.Result{RequeueAfter: retryDelay}
		return s.health, nil
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: s.cluster.Status.Resources.SuperUserSecretRef.Name, Namespace: s.cluster.Namespace}
	if err := s.client.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			s.health.State = pgcConstants.Provisioning
			s.health.Reason = reasonUserSecretPending
			s.health.Message = msgSecretNotFoundYet
			s.health.Phase = provisioningClusterPhase
			s.health.Result = ctrl.Result{RequeueAfter: retryDelay}
			return s.health, nil
		}
		s.health.State = pgcConstants.Failed
		s.health.Reason = reasonUserSecretFailed
		s.health.Message = fmt.Sprintf("Failed to fetch superuser secret: %v", err)
		s.health.Phase = failedClusterPhase
		s.health.Result = ctrl.Result{}
		return s.health, err
	}

	refKey := s.cluster.Status.Resources.SuperUserSecretRef.Key
	if refKey == "" {
		refKey = secretKeyPassword
	}
	if _, ok := secret.Data[refKey]; !ok {
		s.health.State = pgcConstants.Failed
		s.health.Reason = reasonSuperUserSecretFailed
		s.health.Message = fmt.Sprintf(msgFmtSecretMissingKey, refKey)
		s.health.Phase = failedClusterPhase
		s.health.Result = ctrl.Result{}
		return s.health, fmt.Errorf("secret missing key %s", refKey)
	}

	s.health.State = pgcConstants.Ready
	s.health.Reason = reasonSuperUserSecretReady
	s.health.Message = msgSuperuserSecretReady
	s.health.Phase = readyClusterPhase
	s.health.Result = ctrl.Result{}
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

// getMergedConfig overlays PostgresCluster spec on top of the class defaults.
// Class values are used only where the cluster spec is silent.
func getMergedConfig(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) (*MergedConfig, error) {
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
	}

	if result.Instances == nil || result.PostgresVersion == nil || result.Storage == nil {
		return nil, fmt.Errorf("invalid configuration for class %s: instances, postgresVersion and storage are required", class.Name)
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

	return &MergedConfig{Spec: result, CNPG: class.Spec.CNPG}, nil
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec.
// IMPORTANT: any field added here must also appear in normalizeCNPGClusterSpec,
// otherwise spec drift will be silently ignored.
func buildCNPGClusterSpec(cfg *MergedConfig, secretName string, postgresMetricsEnabled bool) cnpgv1.ClusterSpec {
	spec := cnpgv1.ClusterSpec{
		ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *cfg.Spec.PostgresVersion),
		Instances: int(*cfg.Spec.Instances),
		PostgresConfiguration: cnpgv1.PostgresConfiguration{
			Parameters: cfg.Spec.PostgreSQLConfig,
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
	annotations := make(map[string]string)
	if postgresMetricsEnabled {
		annotations = buildPostgresScrapeAnnotations()
	}
	spec.InheritedMetadata = &cnpgv1.EmbeddedObjectMetadata{Annotations: annotations}
	return spec
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

func normalizeCNPGClusterSpec(spec cnpgv1.ClusterSpec, customDefinedParameters map[string]string) normalizedCNPGClusterSpec {
	normalized := normalizedCNPGClusterSpec{
		ImageName:   spec.ImageName,
		Instances:   spec.Instances,
		StorageSize: spec.StorageConfiguration.Size,
		Resources:   spec.Resources,
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
	return normalized
}

// reconcileManagedRoles synchronizes ManagedRoles from PostgresCluster spec to CNPG Cluster managed.roles.
func reconcileManagedRoles(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := log.FromContext(ctx)

	if len(cluster.Spec.ManagedRoles) == 0 {
		logger.Info("No managed roles to reconcile")
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
		logger.Info("CNPG Cluster roles already match desired state, no update needed")
		return nil
	}

	logger.Info("CNPG Cluster roles drift detected, update started",
		"currentCount", len(currentRoles), "desiredCount", len(desiredRoles))

	originalCluster := cnpgCluster.DeepCopy()
	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desiredRoles

	if err := c.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); err != nil {
		return fmt.Errorf("patching CNPG Cluster managed roles: %w", err)
	}
	logger.Info("CNPG Cluster managed roles updated", "roleCount", len(desiredRoles))
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
	logger := log.FromContext(ctx)
	poolerName := poolerResourceName(cluster.Name, poolerType)
	existing := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	logger.Info("CNPG Pooler creation started", "name", poolerName, "type", poolerType)
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
	// Template is always set so that annotation removal is explicit in merge patches.
	// CNPG's Pooler CRD requires template.spec.containers to be present — a minimal
	// named container lets CNPG's podspec builder merge in the real PgBouncer
	// image/command/ports while still carrying our annotations.
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
	logger := log.FromContext(ctx)
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
		logger.Info("CNPG Pooler deletion started", "name", poolerName)
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

	p := string(phase)
	cluster.Status.Phase = &p
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

// generateConfigMap builds a ConfigMap with connection details for the PostgresCluster.
func generateConfigMap(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, secretName string) (*corev1.ConfigMap, error) {
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
	rwExists, err := poolerExists(ctx, c, cluster, readWriteEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to check RW pooler existence: %w", err)
	}
	roExists, err := poolerExists(ctx, c, cluster, readOnlyEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to check RO pooler existence: %w", err)
	}
	if rwExists && roExists {
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

// ensureClusterSecret creates the superuser secret if it doesn't exist and persists the ref to status.
func ensureClusterSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, secretName string, secret *corev1.Secret) error {
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cluster.Namespace}, secret)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
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
	logger := log.FromContext(ctx)
	if cnpgCluster == nil {
		logger.Info("CNPG Cluster not found, skipping deletion")
		return nil
	}
	logger.Info("CNPG Cluster deletion started", "name", cnpgCluster.Name)
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
	logger := log.FromContext(ctx)
	if cluster.GetDeletionTimestamp() == nil {
		logger.Info("PostgresCluster not marked for deletion, skipping finalizer logic")
		return nil
	}
	if !controllerutil.ContainsFinalizer(cluster, PostgresClusterFinalizerName) {
		logger.Info("Finalizer not present on PostgresCluster, skipping finalizer logic")
		return nil
	}

	cnpgCluster := &cnpgv1.Cluster{}
	err := c.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cnpgCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cnpgCluster = nil
			logger.Info("CNPG cluster not found during cleanup")
		} else {
			return fmt.Errorf("fetching CNPG cluster: %w", err)
		}
	}
	logger.Info("Finalizer cleanup started")

	policy := ""
	if cluster.Spec.ClusterDeletionPolicy != nil {
		policy = *cluster.Spec.ClusterDeletionPolicy
	}

	if err := deleteConnectionPoolers(ctx, c, cluster); err != nil {
		return fmt.Errorf("deleting connection poolers: %w", err)
	}

	switch policy {
	case clusterDeletionPolicyDelete:
		logger.Info("ClusterDeletionPolicy 'Delete', CNPG Cluster deletion started")
		if cnpgCluster != nil {
			if err := deleteCNPGCluster(ctx, c, cnpgCluster); err != nil {
				return fmt.Errorf("deleting CNPG Cluster: %w", err)
			}
		} else {
			logger.Info("CNPG Cluster not found, skipping deletion")
		}

	case clusterDeletionPolicyRetain:
		logger.Info("ClusterDeletionPolicy 'Retain', orphaning CNPG Cluster")
		if cnpgCluster != nil {
			originalCNPG := cnpgCluster.DeepCopy()
			refRemoved, err := removeOwnerRef(scheme, cluster, cnpgCluster)
			if err != nil {
				return fmt.Errorf("removing owner reference from CNPG cluster: %w", err)
			}
			if !refRemoved {
				logger.Info("Owner reference already removed from CNPG Cluster, skipping patch")
			}
			if err := patchObject(ctx, c, originalCNPG, cnpgCluster, "CNPGCluster"); err != nil {
				return fmt.Errorf("patching CNPG cluster after removing owner reference: %w", err)
			}
			logger.Info("Removed owner reference from CNPG Cluster")
		}

		// Remove owner reference from the superuser Secret to prevent cascading deletion.
		if cluster.Status.Resources != nil && cluster.Status.Resources.SuperUserSecretRef != nil {
			secretName := cluster.Status.Resources.SuperUserSecretRef.Name
			if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cluster.Namespace}, secret); err != nil {
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("fetching secret during cleanup: %w", err)
				}
				logger.Info("Secret not found, skipping owner reference removal", "secret", secretName)
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
				logger.Info("Removed owner reference from Secret")
			}
		}

	default:
		logger.Info("Unknown ClusterDeletionPolicy", "policy", policy)
	}

	controllerutil.RemoveFinalizer(cluster, PostgresClusterFinalizerName)
	if err := c.Update(ctx, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PostgresCluster already deleted, skipping finalizer update")
			return nil
		}
		return fmt.Errorf("removing finalizer: %w", err)
	}
	rc.emitNormal(cluster, EventCleanupComplete, fmt.Sprintf("Cleanup complete (policy: %s)", policy))
	logger.Info("Finalizer removed, cleanup complete")
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
