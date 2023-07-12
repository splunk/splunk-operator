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

// Instead of passing a zillion arguments to the action of a phase,
// hold them in a context
type reconcileLMInfo struct {
	log               logr.Logger
	resource          *enterpriseApi.LicenseManager
	request           ctrl.Request
	splunkSecret      *corev1.Secret
	events            []corev1.Event
	errorMessage      string
	postSaveCallbacks []func()
}

// match the provisioner.EventPublisher interface
func (info *reconcileLMInfo) publishEvent(stype, reason, message string) {
	info.events = append(info.events, info.resource.NewEvent(stype, reason, message))
}

type licenseManagerStateHandler func(context.Context, *reconcileLMInfo) actionResult

// licenseManagerStateMachine is a finite state machine that manages transitions between
// the states of a splunk Upgrade Path.
type licenseManagerStateMachine struct {
	Resource    *enterpriseApi.LicenseManager
	NextState   enterpriseApi.ProvisioningState
	Reconciler  *LicenseManagerReconciler
	Provisioner provisioner.Provisioner
}

func newProvisioningStateMachine(ctx context.Context, resource *enterpriseApi.LicenseManager,
	reconciler *LicenseManagerReconciler,
	provisioner provisioner.Provisioner,
	haveCreds bool) *licenseManagerStateMachine {
	currentState := resource.Status.Provisioning.State
	r := licenseManagerStateMachine{
		Resource:    resource,
		NextState:   currentState, // Remain in current state by default
		Reconciler:  reconciler,
		Provisioner: provisioner,
	}
	return &r
}

func (hsm *licenseManagerStateMachine) handlers(ctx context.Context) map[enterpriseApi.ProvisioningState]licenseManagerStateHandler {
	return map[enterpriseApi.ProvisioningState]licenseManagerStateHandler{
		enterpriseApi.StateNone:                       hsm.handleNone,
		enterpriseApi.StateLicenseManagerPrepare:      hsm.handleLicenseManagerPrepare,
		enterpriseApi.StateLicenseManagerBackup:       hsm.handleLicenseManagerBackup,
		enterpriseApi.StateLicenseManagerRestore:      hsm.handleLicenseManagerRestore,
		enterpriseApi.StateLicenseManagerUpgrade:      hsm.handleLicenseManagerUpgrade,
		enterpriseApi.StateLicenseManagerVerification: hsm.handleLicenseManagerVerification,
		enterpriseApi.StateLicenseManagerReady:        hsm.handleLicenseManagerReady,
	}
}

func recordLicenseManagerStateBegin(ctx context.Context, resource *enterpriseApi.LicenseManager, state enterpriseApi.ProvisioningState, time metav1.Time) {
	if nextMetric := resource.OperationMetricForState(state); nextMetric != nil {
		if nextMetric.Start.IsZero() || !nextMetric.End.IsZero() {
			*nextMetric = enterpriseApi.OperationMetric{
				Start: time,
			}
		}
	}
}

func recordLicenseManagerStateEnd(ctx context.Context, info *reconcileLMInfo, lm *enterpriseApi.LicenseManager, state enterpriseApi.ProvisioningState, time metav1.Time) (changed bool) {
	if prevMetric := lm.OperationMetricForState(state); prevMetric != nil {
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

func (hsm *licenseManagerStateMachine) updateSplunkProvisioningStateFrom(ctx context.Context,
	initialState enterpriseApi.ProvisioningState,
	info *reconcileLMInfo) actionResult {
	if hsm.NextState != initialState {

		info.log.Info("changing provisioning state",
			"old", initialState,
			"new", hsm.NextState)
		now := metav1.Now()
		recordLicenseManagerStateEnd(ctx, info, hsm.Resource, initialState, now)
		recordLicenseManagerStateBegin(ctx, hsm.Resource, hsm.NextState, now)
		info.postSaveCallbacks = append(info.postSaveCallbacks, func() {
			stateChanges.With(stateChangeMetricLabels(initialState, hsm.NextState)).Inc()
		})
		hsm.Resource.Status.Provisioning.State = hsm.NextState
	}

	return nil
}

func (hsm *licenseManagerStateMachine) ReconcileState(ctx context.Context, info *reconcileLMInfo) (actionRes actionResult) {
	initialState := hsm.Resource.Status.Provisioning.State

	defer func() {
		if overrideAction := hsm.updateSplunkProvisioningStateFrom(ctx, initialState, info); overrideAction != nil {
			actionRes = overrideAction
		}
	}()

	if licenseManagerStateHandler, found := hsm.handlers(ctx)[initialState]; found {
		return licenseManagerStateHandler(ctx, info)
	}

	info.log.Info("No handler found for state", "state", initialState)
	return actionError{fmt.Errorf("No handler found for state \"%s\"", initialState)}
}

func (hsm *licenseManagerStateMachine) handleNone(ctx context.Context, info *reconcileLMInfo) actionResult {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("Upgrade")
	scopedLog.Info("handle None")

	// No state is set, so immediately move to either LicenseManagerPrepare or LicenseManagerError
	hsm.Resource.SetOperationalStatus(enterpriseApi.OperationalStatusPrepareForUpgrade)
	hsm.NextState = enterpriseApi.StateLicenseManagerPrepare
	upgradeLicenseManagerPrepare.Inc()
	return actionComplete{}
}

func (hsm *licenseManagerStateMachine) handleLicenseManagerPrepare(ctx context.Context, info *reconcileLMInfo) actionResult {
	actResult := hsm.Reconciler.actionLicenseManagerPrepare(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateLicenseManagerBackup
	} else {
		hsm.NextState = enterpriseApi.StateLicenseManagerError
	}
	return actResult
}

func (hsm *licenseManagerStateMachine) handleLicenseManagerBackup(ctx context.Context, info *reconcileLMInfo) actionResult {
	actResult := hsm.Reconciler.actionLicenseManagerBackup(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateLicenseManagerUpgrade
	} else {
		hsm.NextState = enterpriseApi.StateLicenseManagerError
	}
	return actResult
}

func (hsm *licenseManagerStateMachine) handleLicenseManagerRestore(ctx context.Context, info *reconcileLMInfo) actionResult {
	actResult := hsm.Reconciler.actionLicenseManagerRestore(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateLicenseManagerVerification
	} else {
		hsm.NextState = enterpriseApi.StateLicenseManagerError
	}
	return actResult
}

func (hsm *licenseManagerStateMachine) handleLicenseManagerUpgrade(ctx context.Context, info *reconcileLMInfo) actionResult {
	actResult := hsm.Reconciler.actionLicenseManagerUpgrade(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateLicenseManagerVerification
	} else {
		hsm.NextState = enterpriseApi.StateLicenseManagerError
	}
	return actResult
}

func (hsm *licenseManagerStateMachine) handleLicenseManagerVerification(ctx context.Context, info *reconcileLMInfo) actionResult {
	actResult := hsm.Reconciler.actionLicenseManagerVerification(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateLicenseManagerReady
	} else {
		hsm.NextState = enterpriseApi.StateLicenseManagerError
	}
	return actResult
}

func (hsm *licenseManagerStateMachine) handleLicenseManagerReady(ctx context.Context, info *reconcileLMInfo) actionResult {
	actResult := hsm.Reconciler.actionLicenseManagerReady(ctx, hsm.Provisioner, info)
	if _, complete := actResult.(actionComplete); complete {
		hsm.NextState = enterpriseApi.StateClusterManagerPrepare
	} else {
		hsm.NextState = enterpriseApi.StateLicenseManagerError
	}

	return actResult
}
