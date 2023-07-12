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

type indexerClusterStateHandler func(context.Context, *reconcileICInfo) actionResult

// Instead of passing a zillion arguments to the action of a phase,
// hold them in a context
type reconcileICInfo struct {
	log               logr.Logger
	resource          *enterpriseApi.IndexerCluster
	request           ctrl.Request
	splunkSecret      *corev1.Secret
	events            []corev1.Event
	errorMessage      string
	postSaveCallbacks []func()
}

// match the provisioner.EventPublisher interface
func (info *reconcileICInfo) publishEvent(stype, reason, message string) {
	info.events = append(info.events, info.resource.NewEvent(stype, reason, message))
}

// indexerClusterStateMachine is a finite state machine that manages transitions between
// the states of a splunk Upgrade Path.
type indexerClusterStateMachine struct {
	Resource    *enterpriseApi.IndexerCluster
	NextState   enterpriseApi.ProvisioningState
	Reconciler  *IndexerClusterReconciler
	Provisioner provisioner.Provisioner
}

func newUpgradeIndexerClusterStateMachine(ctx context.Context, resource *enterpriseApi.IndexerCluster,
	reconciler *IndexerClusterReconciler,
	provisioner provisioner.Provisioner,
	haveCreds bool) *indexerClusterStateMachine {
	currentState := resource.Status.Provisioning.State
	r := indexerClusterStateMachine{
		Resource:    resource,
		NextState:   currentState, // Remain in current state by default
		Reconciler:  reconciler,
		Provisioner: provisioner,
	}
	return &r
}

func (hsm *indexerClusterStateMachine) handlers(ctx context.Context) map[enterpriseApi.ProvisioningState]indexerClusterStateHandler {
	return map[enterpriseApi.ProvisioningState]indexerClusterStateHandler{
		enterpriseApi.StateNone:                       hsm.handleNone,
		enterpriseApi.StateIndexerClusterPrepare:      hsm.handleIndexerClusterPrepare,
		enterpriseApi.StateIndexerClusterBackup:       hsm.handleIndexerClusterBackup,
		enterpriseApi.StateIndexerClusterRestore:      hsm.handleIndexerClusterRestore,
		enterpriseApi.StateIndexerClusterUpgrade:      hsm.handleIndexerClusterUpgrade,
		enterpriseApi.StateIndexerClusterVerification: hsm.handleIndexerClusterVerification,
		enterpriseApi.StateIndexerClusterReady:        hsm.handleIndexerClusterReady,
	}
}

func recordIndexerClusterStateBegin(ctx context.Context, resource *enterpriseApi.IndexerCluster, state enterpriseApi.ProvisioningState, time metav1.Time) {
	if nextMetric := resource.OperationMetricForState(state); nextMetric != nil {
		if nextMetric.Start.IsZero() || !nextMetric.End.IsZero() {
			*nextMetric = enterpriseApi.OperationMetric{
				Start: time,
			}
		}
	}
}

func recordIndexerClusterStateEnd(ctx context.Context, info *reconcileICInfo, resource *enterpriseApi.IndexerCluster, state enterpriseApi.ProvisioningState, time metav1.Time) (changed bool) {
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

func (hsm *indexerClusterStateMachine) updateSplunkProvisioningStateFrom(ctx context.Context, initialState enterpriseApi.ProvisioningState,
	info *reconcileICInfo) actionResult {
	if hsm.NextState != initialState {

		info.log.Info("changing provisioning state",
			"old", initialState,
			"new", hsm.NextState)
		now := metav1.Now()
		recordIndexerClusterStateEnd(ctx, info, hsm.Resource, initialState, now)
		recordIndexerClusterStateBegin(ctx, hsm.Resource, hsm.NextState, now)
		info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
			stateChanges.With(stateChangeMetricLabels(initialState, hsm.NextState)).Inc()
		})
		hsm.Resource.Status.Provisioning.State = hsm.NextState
	}

	return nil
}

func (hsm *indexerClusterStateMachine) ReconcileState(ctx context.Context, info *reconcileICInfo) (actionRes actionResult) {
	initialState := hsm.Resource.Status.Provisioning.State

	defer func() {
		if overrideAction := hsm.updateSplunkProvisioningStateFrom(ctx, initialState, info); overrideAction != nil {
			actionRes = overrideAction
		}
	}()

	if indexerClusterStateHandler, found := hsm.handlers(ctx)[initialState]; found {
		return indexerClusterStateHandler(ctx, info)
	}

	info.log.Info("No handler found for state", "state", initialState)
	return actionError{fmt.Errorf("No handler found for state \"%s\"", initialState)}
}

func (hsm *indexerClusterStateMachine) handleNone(ctx context.Context, info *reconcileICInfo) actionResult {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("Upgrade")
	scopedLog.Info("handle None")

	hsm.Resource.SetOperationalStatus(enterpriseApi.OperationalStatusPrepareForUpgrade)
	hsm.NextState = enterpriseApi.StateLicenseManagerPrepare
	upgradeLicenseManagerPrepare.Inc()
	return actionComplete{}
}

func (hsm *indexerClusterStateMachine) handleIndexerClusterPrepare(ctx context.Context, info *reconcileICInfo) actionResult {
	actResult := hsm.Reconciler.actionIndexerClusterPrepare(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateIndexerClusterBackup
	} else {
		hsm.NextState = enterpriseApi.StateIndexerClusterError
	}
	return actResult
}

func (hsm *indexerClusterStateMachine) handleIndexerClusterBackup(ctx context.Context, info *reconcileICInfo) actionResult {
	actResult := hsm.Reconciler.actionIndexerClusterBackup(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateIndexerClusterUpgrade
	} else {
		hsm.NextState = enterpriseApi.StateIndexerClusterError
	}
	return actResult
}

func (hsm *indexerClusterStateMachine) handleIndexerClusterRestore(ctx context.Context, info *reconcileICInfo) actionResult {
	actResult := hsm.Reconciler.actionIndexerClusterRestore(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateIndexerClusterReady
	} else {
		hsm.NextState = enterpriseApi.StateIndexerClusterError
	}
	return actResult
}

func (hsm *indexerClusterStateMachine) handleIndexerClusterUpgrade(ctx context.Context, info *reconcileICInfo) actionResult {
	actResult := hsm.Reconciler.actionIndexerClusterUpgrade(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateIndexerClusterVerification
	} else {
		hsm.NextState = enterpriseApi.StateIndexerClusterError
	}
	return actResult
}

func (hsm *indexerClusterStateMachine) handleIndexerClusterVerification(ctx context.Context, info *reconcileICInfo) actionResult {
	actResult := hsm.Reconciler.actionIndexerClusterVerification(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateIndexerClusterReady
	} else {
		hsm.NextState = enterpriseApi.StateIndexerClusterError
	}
	return actResult
}

func (hsm *indexerClusterStateMachine) handleIndexerClusterReady(ctx context.Context, info *reconcileICInfo) actionResult {
	actResult := hsm.Reconciler.actionIndexerClusterReady(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateLicenseManagerUpgrade
	} else {
		hsm.NextState = enterpriseApi.StateIndexerClusterError
	}
	return actResult
}
