package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	provisioner "github.com/splunk/splunk-operator/pkg/provisioner/splunk"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type clusterManagerStateHandler func(context.Context, *reconcileCMInfo) actionResult

// Instead of passing a zillion arguments to the action of a phase,
// hold them in a context
type reconcileCMInfo struct {
	log               logr.Logger
	resource          *enterpriseApi.ClusterManager
	request           ctrl.Request
	splunkSecret      *corev1.Secret
	events            []corev1.Event
	errorMessage      string
	postSaveCallbacks []func()
}

// match the provisioner.EventPublisher interface
func (info *reconcileCMInfo) publishEvent(stype, reason, message string) {
	info.events = append(info.events, info.resource.NewEvent(stype, reason, message))
}

// clusterManagerStateMachine is a finite state machine that manages transitions between
// the states of a splunk Upgrade Path.
type clusterManagerStateMachine struct {
	Resource    *enterpriseApi.ClusterManager
	NextState   enterpriseApi.ProvisioningState
	Reconciler  *ClusterManagerReconciler
	Provisioner provisioner.Provisioner
}

func newUpgradeClusterManagerStateMachine(ctx context.Context, resource *enterpriseApi.ClusterManager,
	reconciler *ClusterManagerReconciler,
	provisioner provisioner.Provisioner,
	haveCreds bool) *clusterManagerStateMachine {
	currentState := resource.Status.Provisioning.State
	r := clusterManagerStateMachine{
		Resource:    resource,
		NextState:   currentState, // Remain in current state by default
		Reconciler:  reconciler,
		Provisioner: provisioner,
	}
	return &r
}

func (hsm *clusterManagerStateMachine) handlers(ctx context.Context) map[enterpriseApi.ProvisioningState]clusterManagerStateHandler {
	return map[enterpriseApi.ProvisioningState]clusterManagerStateHandler{
		enterpriseApi.StateNone:                       hsm.handleNone,
		enterpriseApi.StateClusterManagerBackup:       hsm.handleClusterManagerBackup,
		enterpriseApi.StateClusterManagerRestore:      hsm.handleClusterManagerRestore,
		enterpriseApi.StateClusterManagerUpgrade:      hsm.handleClusterManagerUpgrade,
		enterpriseApi.StateClusterManagerVerification: hsm.handleClusterManagerVerification,
		enterpriseApi.StateClusterManagerReady:        hsm.handleClusterManagerReady,
	}
}

func recordClusterManagerStateBegin(ctx context.Context, resource *enterpriseApi.ClusterManager, state enterpriseApi.ProvisioningState, time metav1.Time) {
	if nextMetric := resource.OperationMetricForState(state); nextMetric != nil {
		if nextMetric.Start.IsZero() || !nextMetric.End.IsZero() {
			*nextMetric = enterpriseApi.OperationMetric{
				Start: time,
			}
		}
	}
}

func recordClusterManagerStateEnd(ctx context.Context, info *reconcileCMInfo, resource *enterpriseApi.ClusterManager, state enterpriseApi.ProvisioningState, time metav1.Time) (changed bool) {
	if prevMetric := resource.OperationMetricForState(state); prevMetric != nil {
		if !prevMetric.Start.IsZero() && prevMetric.End.IsZero() {
			prevMetric.End = time
			info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
				observer := stateTime[state].With(crMetricLabels(info.request))
				observer.Observe(prevMetric.Duration().Seconds())
			})
			changed = true
		}
	}
	return
}

func (hsm *clusterManagerStateMachine) updateSplunkProvisioningStateFrom(ctx context.Context, initialState enterpriseApi.ProvisioningState,
	info *reconcileCMInfo) actionResult {
	if hsm.NextState != initialState {

		info.log.Info("changing provisioning state",
			"old", initialState,
			"new", hsm.NextState)
		now := metav1.Now()
		recordClusterManagerStateEnd(ctx, info, hsm.Resource, initialState, now)
		recordClusterManagerStateBegin(ctx, hsm.Resource, hsm.NextState, now)
		info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
			stateChanges.With(stateChangeMetricLabels(initialState, hsm.NextState)).Inc()
		})
		hsm.Resource.Status.Provisioning.State = hsm.NextState
	}

	return nil
}

func (hsm *clusterManagerStateMachine) ReconcileState(ctx context.Context, info *reconcileCMInfo) (actionRes actionResult) {
	initialState := hsm.Resource.Status.Provisioning.State

	defer func() {
		if overrideAction := hsm.updateSplunkProvisioningStateFrom(ctx, initialState, info); overrideAction != nil {
			actionRes = overrideAction
		}
	}()

	if clusterManagerStateHandler, found := hsm.handlers(ctx)[initialState]; found {
		return clusterManagerStateHandler(ctx, info)
	}

	info.log.Info("No handler found for state", "state", initialState)
	return actionError{fmt.Errorf("No handler found for state \"%s\"", initialState)}
}

func (hsm *clusterManagerStateMachine) handleNone(ctx context.Context, info *reconcileCMInfo) actionResult {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("Upgrade")
	scopedLog.Info("handle None")
	// No state is set, so immediately move to either LicenseManagerPrepare or LicenseManagerError

	hsm.Resource.SetOperationalStatus(enterpriseApi.OperationalStatusPrepareForUpgrade)
	hsm.NextState = enterpriseApi.StateLicenseManagerPrepare
	upgradeLicenseManagerPrepare.Inc()
	return actionComplete{}
}

func (hsm *clusterManagerStateMachine) handleClusterManagerPrepare(ctx context.Context, info *reconcileCMInfo) actionResult {
	actResult := hsm.Reconciler.actionClusterManagerPrepare(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateClusterManagerBackup
	} else {
		hsm.NextState = enterpriseApi.StateClusterManagerError
	}
	return actResult
}

func (hsm *clusterManagerStateMachine) handleClusterManagerBackup(ctx context.Context, info *reconcileCMInfo) actionResult {
	actResult := hsm.Reconciler.actionClusterManagerBackup(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateClusterManagerUpgrade
	} else {
		hsm.NextState = enterpriseApi.StateClusterManagerError
	}
	return actResult
}

func (hsm *clusterManagerStateMachine) handleClusterManagerRestore(ctx context.Context, info *reconcileCMInfo) actionResult {
	actResult := hsm.Reconciler.actionClusterManagerRestore(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateClusterManagerVerification
	} else {
		hsm.NextState = enterpriseApi.StateClusterManagerError
	}
	return actResult
}

func (hsm *clusterManagerStateMachine) handleClusterManagerUpgrade(ctx context.Context, info *reconcileCMInfo) actionResult {
	actResult := hsm.Reconciler.actionClusterManagerUpgrade(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateClusterManagerVerification
	} else {
		hsm.NextState = enterpriseApi.StateClusterManagerError
	}
	return actResult
}

func (hsm *clusterManagerStateMachine) handleClusterManagerVerification(ctx context.Context, info *reconcileCMInfo) actionResult {
	actResult := hsm.Reconciler.actionClusterManagerVerification(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateClusterManagerReady
	} else {
		hsm.NextState = enterpriseApi.StateClusterManagerError
	}
	return actResult
}

func (hsm *clusterManagerStateMachine) handleClusterManagerReady(ctx context.Context, info *reconcileCMInfo) actionResult {
	actResult := hsm.Reconciler.actionClusterManagerReady(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateMonitoringConsolePrepare
	} else {
		hsm.NextState = enterpriseApi.StateClusterManagerError
	}
	return actResult
}
