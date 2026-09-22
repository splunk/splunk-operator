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

package licensemanager

import (
	"context"
	"fmt"
	"reflect"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/appframework"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	// TODO: Remove this temporary dependency once all CRs have migrated from enterprise.
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
)

const pauseRetryDelay = 30 * time.Second

// apply owns the request-level LicenseManager reconciliation boundary.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "LicenseManager", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	instance := &enterpriseApi.LicenseManager{}
	if err := client.Get(ctx, namespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not load license manager data: %w", err)
	}

	if instance.GetAnnotations()[enterpriseApi.LicenseManagerPausedAnnotation] == "true" {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: true, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update paused status", "error", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
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
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)
	result, err := ApplyLicenseManager(ctx, client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}

	fresh := &enterpriseApi.LicenseManager{}
	if fetchErr := client.Get(ctx, namespacedName, fresh); fetchErr != nil {
		if apierrors.IsNotFound(fetchErr) {
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

// ApplyLicenseManager reconciles the state for the Splunk Enterprise license manager.
var ApplyLicenseManager = applyLicenseManager

func applyLicenseManager(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplyLicenseManager")

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "LicenseManager"

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.LicenseManagerPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = ValidateLicenseManagerSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateLicenseManagerSpec", fmt.Sprintf("validate license manager spec failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "License Manager spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "License Manager spec validation failed", err)
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
		err := appframework.InitAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	// create or update general config resources
	_, err = k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkLicenseManager)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, licenseManagerURL(cr, &cr.Spec.CommonSplunkSpec), false)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap during deletion")
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = appframework.UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, splcommon.SplunkLicenseManager)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up resources during deletion")
				return result, err
			}
		}

		k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkLicenseManager)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "Delete", fmt.Sprintf("delete custom resource failed %s", err.Error()))
		}
		return result, err
	}

	// create or update a service
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkLicenseManager, false))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update service")
		return result, err
	}

	// create or update statefulset
	statefulSet, err := GetLicenseManagerStatefulSet(ctx, client, cr)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = k8sops.ValidateMonitoringConsoleRef(ctx, client, statefulSet, licenseManagerURL(cr, &cr.Spec.CommonSplunkSpec))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	// Check for license-related pod failures before updating
	if err = CheckLicenseRelatedPodFailures(ctx, client, cr, statefulSet); err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "License validation failed")
		return result, fmt.Errorf("license check: %w", err)
	}

	mgr := k8sops.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, 1)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods")
		return result, err
	}
	setPhaseAndConditions(phase, "")

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, licenseManagerURL(cr, &cr.Spec.CommonSplunkSpec), true)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap")
			return result, err
		}
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkMonitoringConsole, cr.GetNamespace())}
		err = k8sops.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			logger.ErrorContext(ctx, "error in deleting automated MonitoringConsole resource", "error", err)
		}

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := telapp.AddTelApp(ctx, podExecClient, splcommon.NumberOfLicenseManagerReplicas, cr)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to install Telemetry app")
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}

		finalResult := appframework.HandleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// trigger ClusterManager reconcile by changing the splunk/image-tag annotation
		err = ChangeClusterManagerAnnotations(ctx, client, cr)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to trigger Cluster Manager reconciliation")
			return result, err
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// getLicenseManagerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func GetLicenseManagerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNames(splcommon.SplunkLicenseManager, cr.GetName(), cr.GetNamespace(), 1)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	ss, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkLicenseManager, 1, []corev1.EnvVar{})
	if err != nil {
		return ss, err
	}
	certs.InjectCertMounts(&ss.Spec.Template, certMounts)

	// Setup App framework staging volume for apps
	resources.SetupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateLicenseManagerSpec checks validity and makes default updates to a LicenseManagerSpec, and returns error if something is wrong.
func ValidateLicenseManagerSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) error {

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := appframework.ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return reconcileutil.ValidateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

func licenseManagerURL(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) []corev1.EnvVar {
	if spec.LicenseManagerRef.Name != "" {
		licenseManagerURL := splcommon.GetSplunkServiceName(splcommon.SplunkLicenseManager, spec.LicenseManagerRef.Name, false)
		if spec.LicenseManagerRef.Namespace != "" {
			licenseManagerURL = splcommon.GetServiceFQDN(spec.LicenseManagerRef.Namespace, licenseManagerURL)
		}
		return []corev1.EnvVar{{Name: splcommon.LicenseManagerURL, Value: licenseManagerURL}}
	}
	return []corev1.EnvVar{{Name: splcommon.LicenseManagerURL, Value: splcommon.GetSplunkServiceName(splcommon.SplunkLicenseManager, cr.GetName(), false)}}
}
