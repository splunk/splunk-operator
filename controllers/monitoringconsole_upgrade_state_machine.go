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

type monitoringConsoleStateHandler func(context.Context, *reconcileMCInfo) actionResult

// Instead of passing a zillion arguments to the action of a phase,
// hold them in a context
type reconcileMCInfo struct {
	log               logr.Logger
	resource          *enterpriseApi.MonitoringConsole
	request           ctrl.Request
	splunkSecret      *corev1.Secret
	events            []corev1.Event
	errorMessage      string
	postSaveCallbacks []func()
}

// match the provisioner.EventPublisher interface
func (info *reconcileMCInfo) publishEvent(stype, reason, message string) {
	info.events = append(info.events, info.resource.NewEvent(stype, reason, message))
}

// monitoringConsoleStateMachine is a finite state machine that manages transitions between
// the states of a splunk Upgrade Path.
type monitoringConsoleStateMachine struct {
	Resource    *enterpriseApi.MonitoringConsole
	NextState   enterpriseApi.ProvisioningState
	Reconciler  *MonitoringConsoleReconciler
	Provisioner provisioner.Provisioner
}

func newUpgradeMonitoringConsoleStateMachine(ctx context.Context, resource *enterpriseApi.MonitoringConsole,
	reconciler *MonitoringConsoleReconciler,
	provisioner provisioner.Provisioner,
	haveCreds bool) *monitoringConsoleStateMachine {
	currentState := resource.Status.Provisioning.State
	r := monitoringConsoleStateMachine{
		Resource:    resource,
		NextState:   currentState, // Remain in current state by default
		Reconciler:  reconciler,
		Provisioner: provisioner,
	}
	return &r
}

func (hsm *monitoringConsoleStateMachine) handlers(ctx context.Context) map[enterpriseApi.ProvisioningState]monitoringConsoleStateHandler {
	return map[enterpriseApi.ProvisioningState]monitoringConsoleStateHandler{
		enterpriseApi.StateNone:                          hsm.handleNone,
		enterpriseApi.StateMonitoringConsolePrepare:      hsm.handleMonitoringConsolePrepare,
		enterpriseApi.StateMonitoringConsoleBackup:       hsm.handleMonitoringConsoleBackup,
		enterpriseApi.StateMonitoringConsoleRestore:      hsm.handleMonitoringConsoleRestore,
		enterpriseApi.StateMonitoringConsoleUpgrade:      hsm.handleMonitoringConsoleUpgrade,
		enterpriseApi.StateMonitoringConsoleVerification: hsm.handleMonitoringConsoleVerification,
		enterpriseApi.StateMonitoringConsoleReady:        hsm.handleMonitoringConsoleReady,
	}
}

func recordMonitoringConsoleStateBegin(ctx context.Context, resource *enterpriseApi.MonitoringConsole, state enterpriseApi.ProvisioningState, time metav1.Time) {
	if nextMetric := resource.OperationMetricForState(state); nextMetric != nil {
		if nextMetric.Start.IsZero() || !nextMetric.End.IsZero() {
			*nextMetric = enterpriseApi.OperationMetric{
				Start: time,
			}
		}
	}
}

func recordMonitoringConsoleStateEnd(ctx context.Context, info *reconcileMCInfo, resource *enterpriseApi.MonitoringConsole, state enterpriseApi.ProvisioningState, time metav1.Time) (changed bool) {
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

func (hsm *monitoringConsoleStateMachine) updateSplunkProvisioningStateFrom(ctx context.Context, initialState enterpriseApi.ProvisioningState,
	info *reconcileMCInfo) actionResult {
	if hsm.NextState != initialState {

		info.log.Info("changing provisioning state",
			"old", initialState,
			"new", hsm.NextState)
		now := metav1.Now()
		recordMonitoringConsoleStateEnd(ctx, info, hsm.Resource, initialState, now)
		recordMonitoringConsoleStateBegin(ctx, hsm.Resource, hsm.NextState, now)
		info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
			stateChanges.With(stateChangeMetricLabels(initialState, hsm.NextState)).Inc()
		})
		hsm.Resource.Status.Provisioning.State = hsm.NextState
	}

	return nil
}

func (hsm *monitoringConsoleStateMachine) ReconcileState(ctx context.Context, info *reconcileMCInfo) (actionRes actionResult) {
	initialState := hsm.Resource.Status.Provisioning.State

	defer func() {
		if overrideAction := hsm.updateSplunkProvisioningStateFrom(ctx, initialState, info); overrideAction != nil {
			actionRes = overrideAction
		}
	}()

	if monitoringConsoleStateHandler, found := hsm.handlers(ctx)[initialState]; found {
		return monitoringConsoleStateHandler(ctx, info)
	}

	info.log.Info("No handler found for state", "state", initialState)
	return actionError{fmt.Errorf("No handler found for state \"%s\"", initialState)}
}

func (hsm *monitoringConsoleStateMachine) handleNone(ctx context.Context, info *reconcileMCInfo) actionResult {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("Upgrade")
	scopedLog.Info("handle None")

	// No state is set, so immediately move to either LicenseManagerPrepare or LicenseManagerError
	hsm.Resource.SetOperationalStatus(enterpriseApi.OperationalStatusPrepareForUpgrade)
	hsm.NextState = enterpriseApi.StateLicenseManagerPrepare
	upgradeLicenseManagerPrepare.Inc()

	return actionComplete{}
}

func (hsm *monitoringConsoleStateMachine) handleMonitoringConsolePrepare(ctx context.Context, info *reconcileMCInfo) actionResult {
	actResult := hsm.Reconciler.actionMonitoringConsolePrepare(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleBackup
	} else {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleError
	}
	return actResult
}

func (hsm *monitoringConsoleStateMachine) handleMonitoringConsoleBackup(ctx context.Context, info *reconcileMCInfo) actionResult {
	actResult := hsm.Reconciler.actionMonitoringConsoleBackup(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleUpgrade
	} else {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleError
	}
	return actResult
}

func (hsm *monitoringConsoleStateMachine) handleMonitoringConsoleRestore(ctx context.Context, info *reconcileMCInfo) actionResult {
	actResult := hsm.Reconciler.actionMonitoringConsoleRestore(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleReady
	} else {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleError
	}
	return actResult
}

func (hsm *monitoringConsoleStateMachine) handleMonitoringConsoleUpgrade(ctx context.Context, info *reconcileMCInfo) actionResult {
	actResult := hsm.Reconciler.actionMonitoringConsoleUpgrade(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleVerification
	} else {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleError
	}
	return actResult
}

func (hsm *monitoringConsoleStateMachine) handleMonitoringConsoleVerification(ctx context.Context, info *reconcileMCInfo) actionResult {
	actResult := hsm.Reconciler.actionMonitoringConsoleVerification(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleReady
	} else {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleError
	}
	return actResult
}

func (hsm *monitoringConsoleStateMachine) handleMonitoringConsoleReady(ctx context.Context, info *reconcileMCInfo) actionResult {
	actResult := hsm.Reconciler.actionMonitoringConsoleReady(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateSearchHeadClusterPrepare
	} else {
		hsm.NextState = enterpriseApi.StateMonitoringConsoleError
	}
	return actResult
}
