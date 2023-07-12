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

type searcHeadConsoleStateHandler func(context.Context, *reconcileSCInfo) actionResult

// Instead of passing a zillion arguments to the action of a phase,
// hold them in a context
type reconcileSCInfo struct {
	log               logr.Logger
	resource          *enterpriseApi.SearchHeadCluster
	request           ctrl.Request
	splunkSecret      *corev1.Secret
	events            []corev1.Event
	errorMessage      string
	postSaveCallbacks []func()
}

// match the provisioner.EventPublisher interface
func (info *reconcileSCInfo) publishEvent(stype, reason, message string) {
	info.events = append(info.events, info.resource.NewEvent(stype, reason, message))
}

// searchHeadClusterStateMachine is a finite state machine that manages transitions between
// the states of a splunk Upgrade Path.
type searchHeadClusterStateMachine struct {
	Resource    *enterpriseApi.SearchHeadCluster
	NextState   enterpriseApi.ProvisioningState
	Reconciler  *SearchHeadClusterReconciler
	Provisioner provisioner.Provisioner
}

func newUpgradeSearchHeadClusterStateMachine(ctx context.Context, resource *enterpriseApi.SearchHeadCluster,
	reconciler *SearchHeadClusterReconciler,
	provisioner provisioner.Provisioner,
	haveCreds bool) *searchHeadClusterStateMachine {
	currentState := resource.Status.Provisioning.State
	r := searchHeadClusterStateMachine{
		Resource:    resource,
		NextState:   currentState, // Remain in current state by default
		Reconciler:  reconciler,
		Provisioner: provisioner,
	}
	return &r
}

func (hsm *searchHeadClusterStateMachine) handlers(ctx context.Context) map[enterpriseApi.ProvisioningState]searcHeadConsoleStateHandler {
	return map[enterpriseApi.ProvisioningState]searcHeadConsoleStateHandler{
		enterpriseApi.StateNone:                          hsm.handleNone,
		enterpriseApi.StateSearchHeadClusterPrepare:      hsm.handleSearchHeadClusterPrepare,
		enterpriseApi.StateSearchHeadClusterBackup:       hsm.handleSearchHeadClusterBackup,
		enterpriseApi.StateSearchHeadClusterRestore:      hsm.handleSearchHeadClusterRestore,
		enterpriseApi.StateSearchHeadClusterUpgrade:      hsm.handleSearchHeadClusterUpgrade,
		enterpriseApi.StateSearchHeadClusterVerification: hsm.handleSearchHeadClusterVerification,
		enterpriseApi.StateSearchHeadClusterReady:        hsm.handleSearchHeadClusterReady,
	}
}

func recordSearchHeadClusterStateBegin(ctx context.Context, resource *enterpriseApi.SearchHeadCluster, state enterpriseApi.ProvisioningState, time metav1.Time) {
	if nextMetric := resource.OperationMetricForState(state); nextMetric != nil {
		if nextMetric.Start.IsZero() || !nextMetric.End.IsZero() {
			*nextMetric = enterpriseApi.OperationMetric{
				Start: time,
			}
		}
	}
}

func recordSearchHeadClusterStateEnd(ctx context.Context, info *reconcileSCInfo, resource *enterpriseApi.SearchHeadCluster, state enterpriseApi.ProvisioningState, time metav1.Time) (changed bool) {
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

func (hsm *searchHeadClusterStateMachine) updateSplunkProvisioningStateFrom(ctx context.Context, initialState enterpriseApi.ProvisioningState,
	info *reconcileSCInfo) actionResult {
	if hsm.NextState != initialState {

		info.log.Info("changing provisioning state",
			"old", initialState,
			"new", hsm.NextState)
		now := metav1.Now()
		recordSearchHeadClusterStateEnd(ctx, info, hsm.Resource, initialState, now)
		recordSearchHeadClusterStateBegin(ctx, hsm.Resource, hsm.NextState, now)
		info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
			stateChanges.With(stateChangeMetricLabels(initialState, hsm.NextState)).Inc()
		})
		hsm.Resource.Status.Provisioning.State = hsm.NextState

	}

	return nil
}

func (hsm *searchHeadClusterStateMachine) ReconcileState(ctx context.Context, info *reconcileSCInfo) (actionRes actionResult) {
	initialState := hsm.Resource.Status.Provisioning.State

	defer func() {
		if overrideAction := hsm.updateSplunkProvisioningStateFrom(ctx, initialState, info); overrideAction != nil {
			actionRes = overrideAction
		}
	}()

	if searcHeadConsoleStateHandler, found := hsm.handlers(ctx)[initialState]; found {
		return searcHeadConsoleStateHandler(ctx, info)
	}

	info.log.Info("No handler found for state", "state", initialState)
	return actionError{fmt.Errorf("No handler found for state \"%s\"", initialState)}
}

func (hsm *searchHeadClusterStateMachine) handleNone(ctx context.Context, info *reconcileSCInfo) actionResult {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("Upgrade")
	scopedLog.Info("handle None")
	// No state is set, so immediately move to either Prapare or Error
	hsm.Resource.SetOperationalStatus(enterpriseApi.OperationalStatusPrepareForUpgrade)
	hsm.NextState = enterpriseApi.StateLicenseManagerPrepare
	upgradeLicenseManagerPrepare.Inc()
	return actionComplete{}
}

func (hsm *searchHeadClusterStateMachine) handleSearchHeadClusterPrepare(ctx context.Context, info *reconcileSCInfo) actionResult {
	actResult := hsm.Reconciler.actionSearchHeadClusterPrepare(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterBackup
	} else {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterError
	}
	return actResult
}

func (hsm *searchHeadClusterStateMachine) handleSearchHeadClusterBackup(ctx context.Context, info *reconcileSCInfo) actionResult {
	actResult := hsm.Reconciler.actionSearchHeadClusterBackup(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterUpgrade
	} else {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterError
	}
	return actResult
}

func (hsm *searchHeadClusterStateMachine) handleSearchHeadClusterRestore(ctx context.Context, info *reconcileSCInfo) actionResult {
	actResult := hsm.Reconciler.actionSearchHeadClusterRestore(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterReady
	} else {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterError
	}
	return actResult
}

func (hsm *searchHeadClusterStateMachine) handleSearchHeadClusterUpgrade(ctx context.Context, info *reconcileSCInfo) actionResult {
	actResult := hsm.Reconciler.actionSearchHeadClusterUpgrade(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterVerification
	} else {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterError
	}
	return actResult
}

func (hsm *searchHeadClusterStateMachine) handleSearchHeadClusterVerification(ctx context.Context, info *reconcileSCInfo) actionResult {
	actResult := hsm.Reconciler.actionSearchHeadClusterVerification(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterReady
	} else {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterError
	}
	return actResult
}

func (hsm *searchHeadClusterStateMachine) handleSearchHeadClusterReady(ctx context.Context, info *reconcileSCInfo) actionResult {
	actResult := hsm.Reconciler.actionSearchHeadClusterReady(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateIndexerClusterPrepare
	} else {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterError
	}
	return actResult
}
