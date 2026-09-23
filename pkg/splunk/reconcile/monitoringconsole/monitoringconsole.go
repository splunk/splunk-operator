// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package monitoringconsole

import (
	"context"
	"fmt"
	"reflect"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/appframework"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const monitoringConsoleConfigRev = "monitoringConsoleConfigRev"

// apply owns the request-level MonitoringConsole reconciliation boundary.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "MonitoringConsole", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	// Fetch the MonitoringConsole
	instance := &enterpriseApi.MonitoringConsole{}
	if err := client.Get(ctx, namespacedName, instance); err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request. Owned objects are automatically
			// garbage collected. For additional cleanup logic use
			// finalizers. Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("could not load monitoring console data: %w", err)
	}

	// If the reconciliation is paused, set the Paused condition and requeue
	if instance.GetAnnotations()[enterpriseApi.MonitoringConsolePausedAnnotation] == "true" {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: true, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update paused status", "error", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true, RequeueAfter: splcommon.PauseRetryDelay}, nil
	} else if condition := meta.FindStatusCondition(instance.Status.Conditions, string(enterpriseApi.ConditionPaused)); condition != nil && condition.Status == metav1.ConditionTrue {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: false, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update unpaused status", "error", err)
			return reconcile.Result{}, err
		}
	}

	logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())
	// Pass event recorder through context
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)
	result, err := ApplyMonitoringConsole(ctx, client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}

	fresh := &enterpriseApi.MonitoringConsole{}
	if fetchErr := client.Get(ctx, namespacedName, fresh); fetchErr != nil {
		if k8serrors.IsNotFound(fetchErr) {
			return result, nil
		}
		logger.WarnContext(ctx, "failed to refetch CR for stalled condition update", "error", fetchErr)
		return result, fetchErr
	}
	oldConditions := append([]metav1.Condition(nil), fresh.Status.Conditions...)
	if message, ok := splcommon.TerminalMessage(err); ok {
		reason, _ := splcommon.TerminalReason(err)
		fresh.Status.Conditions = splcommon.UpsertStalledCondition(fresh.Status.Conditions, reason, message, fresh.GetGeneration())
	} else {
		fresh.Status.Conditions = splcommon.ClearStalledCondition(fresh.Status.Conditions, fresh.GetGeneration())
	}
	eventPublisher, publisherErr := k8sops.NewK8EventPublisherWithRecorder(recorder, fresh)
	if publisherErr != nil {
		logger.WarnContext(ctx, "failed to create event publisher", "error", publisherErr)
		return result, publisherErr
	}
	k8sops.EmitStalledTransitionEvents(ctx, eventPublisher, fresh.GetName(), oldConditions, fresh.Status.Conditions)
	if updateErr := client.Status().Update(ctx, fresh); updateErr != nil {
		logger.WarnContext(ctx, "failed to upsert stalled condition", "error", updateErr)
		return result, updateErr
	}
	if _, ok := splcommon.TerminalMessage(err); ok {
		return reconcile.Result{}, err
	}
	return result, err
}

// Apply is the request-level entry point used by the controller.
var Apply = apply

// ApplyMonitoringConsole is the operation seam used by focused reconciliation tests.
var ApplyMonitoringConsole = applyMonitoringConsole

// applyMonitoringConsole reconciles the StatefulSet for a MonitoringConsole.
func applyMonitoringConsole(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{Requeue: true, RequeueAfter: 5 * time.Second}
	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "MonitoringConsole"

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.MonitoringConsolePausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		status := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = status.Phase
		cr.Status.Conditions = status.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateMonitoringConsoleSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonValidateSpecFailed, fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Monitoring Console spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "Monitoring Console spec validation failed", err)
	}

	// If needed, Migrate the app framework status
	err = appframework.CheckAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed")
		return result, err
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		if err := appframework.InitAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext); err != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonAppFrameworkInitFailed, fmt.Sprintf("App framework initialization failed for %s — check operator logs", cr.GetName()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-monitoring-console", cr.GetName())

	// create or update general config resources
	if _, err = k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkMonitoringConsole); err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonApplySplunkConfigFailed, fmt.Sprintf("Failed to apply general config for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = appframework.UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, appframework.SplunkLicenseManager)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up resources during deletion")
				return result, err
			}
		}
		terminating, checkErr := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && checkErr != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
		} else {
			result.Requeue = false
		}
		return result, checkErr
	}

	// create or update a headless service
	if err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkMonitoringConsole, true)); err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonApplyServiceFailed, fmt.Sprintf("Failed to apply headless service for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service")
		return result, err
	}

	// create or update a regular service
	if err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkMonitoringConsole, false)); err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonApplyServiceFailed, fmt.Sprintf("Failed to apply regular service for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update regular service")
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getMonitoringConsoleStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonStatefulSetFailed, fmt.Sprintf("Failed to get monitoring console statefulset for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		// check if the Monitoring Console is ready for version upgrade, if required
		continueReconcile, validationErr := upgrade.UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if validationErr != nil || !continueReconcile {
			if validationErr != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed")
			} else {
				// waiting on a dependency (e.g. ClusterManager recycling) is not an error,
				// so don't leave the earlier-staged PhaseError as the persisted status
				setPhaseAndConditions(enterpriseApi.PhasePending, "Waiting for upgrade path dependency to become ready")
			}
			return result, validationErr
		}
	}

	phase, err := (&k8sops.DefaultStatefulSetPodManager{}).Update(ctx, client, statefulSet, 1)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonStatefulSetUpdateFailed, fmt.Sprintf("Failed to update statefulset for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods")
		return result, err
	}
	setPhaseAndConditions(phase, "")

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		result = *appframework.HandleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}
	return result, nil
}

// getMonitoringConsoleStatefulSet returns the desired MonitoringConsole StatefulSet.
func getMonitoringConsoleStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (*appsv1.StatefulSet, error) {
	// get generic statefulset for Splunk Enterprise objects
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNames(splcommon.SplunkMonitoringConsole, cr.GetName(), cr.GetNamespace(), 1)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	statefulSet, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkMonitoringConsole, 1, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}
	certs.InjectCertMounts(&statefulSet.Spec.Template, certMounts)

	configMapName := splutil.GetSplunkMonitoringconsoleConfigMapName(cr.GetName(), splcommon.SplunkMonitoringConsole)
	// use MC configmap as EnvFrom source
	statefulSet.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{
		ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: configMapName}},
	}}

	// update podTemplate annotation with configMap resource version
	configMap, err := k8sops.GetMCConfigMap(ctx, client, cr, types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName})
	if err != nil {
		return nil, err
	}

	statefulSet.Spec.Template.Annotations[monitoringConsoleConfigRev] = splutil.ConfigDataHash(configMap.Data)

	// Setup App framework staging volume for apps
	resources.SetupAppsStagingVolume(ctx, client, cr, &statefulSet.Spec.Template, &cr.Spec.AppFrameworkConfig)
	return statefulSet, nil
}

// validateMonitoringConsoleSpec checks validity and makes default updates to a MonitoringConsole, and returns error if something is wrong.
func validateMonitoringConsoleSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) error {
	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		if err := appframework.ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind); err != nil {
			return err
		}
	}
	return reconcileutil.ValidateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}
