// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package enterprise

import (
	"context"
	"fmt"
	"reflect"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
)

// newSplunkClientFunc is a package-level variable for creating Splunk clients, allowing test injection.
var newSplunkClientFunc = splclient.NewSplunkClient

// ApplyLicenseManager reconciles the state for the Splunk Enterprise license manager.
func ApplyLicenseManager(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplyLicenseManager")

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "LicenseManager"

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetDeletionTimestamp() == nil &&
		cr.GetAnnotations()[enterpriseApi.LicenseManagerPausedAnnotation] == "true"
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
	updateStatusOnReturn := true
	defer func() {
		if updateStatusOnReturn {
			updateCRStatus(ctx, client, cr, &err)
		}
	}()

	// Deletion finalization must run before normal reconciliation. A namespace
	// with a deletion timestamp rejects creation of new namespaced resources,
	// so validation, migration, remote app initialization, and ApplySplunkConfig
	// cannot be prerequisites for removing the CR finalizer.
	if cr.GetDeletionTimestamp() != nil {
		result, err = finalizeLicenseManagerDeletion(
			ctx,
			client,
			cr,
			eventPublisher,
			setPhaseAndConditions,
			result,
		)
		// Successful finalization removes the finalizer and allows the API
		// server to delete the CR immediately. A deferred status update would
		// race that deletion. Retain status reporting only when finalization
		// itself failed and the CR remains actionable.
		if err == nil {
			updateStatusOnReturn = false
		}
		return result, err
	}

	// validate and updates defaults for CR
	err = validateLicenseManagerSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateLicenseManagerSpec", fmt.Sprintf("validate license manager spec failed %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "License Manager spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "License Manager spec validation failed", err)
	}

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed")
		return result, err
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	// create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkLicenseManager)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// The StatefulSet uses this headless Service for stable per-Pod network
	// identity. The license-health check below addresses the Pod through that
	// identity, so reconcile the Service before the StatefulSet and REST call.
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseManager, true))
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonApplyServiceFailed, fmt.Sprintf("Failed to apply headless service for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service")
		return result, err
	}

	// create or update the client-facing service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseManager, false))
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonApplyServiceFailed, fmt.Sprintf("Failed to apply regular service for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update service")
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getLicenseManagerStatefulSet(ctx, client, cr)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, getLicenseManagerURL(cr, &cr.Spec.CommonSplunkSpec))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	// Check for license-related pod failures before updating
	if err = checkLicenseRelatedPodFailures(ctx, client, cr, statefulSet); err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "License validation failed")
		return result, fmt.Errorf("license check: %w", err)
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, 1)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods")
		return result, err
	}
	setPhaseAndConditions(phase, "")

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getLicenseManagerURL(cr, &cr.Spec.CommonSplunkSpec), true)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap")
			return result, err
		}
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			logger.ErrorContext(ctx, "error in deleting automated MonitoringConsole resource", "error", err)
		}

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := addTelApp(ctx, podExecClient, numberOfLicenseMasterReplicas, cr)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to install Telemetry app")
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}

		finalResult := handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// trigger ClusterManager reconcile by changing the splunk/image-tag annotation
		err = changeClusterManagerAnnotations(ctx, client, cr)
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

// finalizeLicenseManagerDeletion performs only deletion-safe operations. It
// must not create replacement namespace content because Kubernetes rejects
// creates after the Namespace enters Terminating.
func finalizeLicenseManagerDeletion(
	ctx context.Context,
	client splcommon.ControllerClient,
	cr *enterpriseApi.LicenseManager,
	eventPublisher *K8EventPublisher,
	setPhaseAndConditions func(enterpriseApi.Phase, string),
	result reconcile.Result,
) (reconcile.Result, error) {
	setPhaseAndConditions(
		enterpriseApi.PhaseTerminating,
		"Resource is being deleted",
	)

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		if _, err := ApplyMonitoringConsoleEnvConfigMap(
			ctx,
			client,
			cr.GetNamespace(),
			cr.GetName(),
			cr.Spec.MonitoringConsoleRef.Name,
			getLicenseManagerURL(cr, &cr.Spec.CommonSplunkSpec),
			false,
		); err != nil {
			setPhaseAndConditions(
				enterpriseApi.PhaseError,
				"Failed to update Monitoring Console env ConfigMap during deletion",
			)
			return result, err
		}
	}

	// If this is the last of its kind getting deleted, remove the entry for
	// this CR type from the shared app-framework ConfigMap. If the ConfigMap
	// has already been removed, cleanup is already complete.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		if err := UpdateOrRemoveEntryFromConfigMapLocked(
			ctx,
			client,
			cr,
			SplunkLicenseManager,
		); err != nil {
			setPhaseAndConditions(
				enterpriseApi.PhaseError,
				"Failed to clean up resources during deletion",
			)
			return result, err
		}
	}

	// Missing Secrets or StatefulSets are expected when the namespace
	// controller is deleting resources concurrently. This cleanup remains
	// best-effort, matching the existing LicenseManager deletion contract.
	_ = DeleteOwnerReferencesForResources(
		ctx,
		client,
		cr,
		SplunkLicenseManager,
	)

	_, err := splctrl.CheckForDeletion(ctx, cr, client)
	if err != nil {
		eventPublisher.Warning(
			ctx,
			EventReasonDeleteFailed,
			fmt.Sprintf(
				"Failed to delete custom resource %s — check operator logs",
				cr.GetName(),
			),
		)
		return result, err
	}

	result.Requeue = false
	return result, nil
}

// getLicenseManagerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getLicenseManagerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseManager, 1, []corev1.EnvVar{}, certMounts)
	if err != nil {
		return ss, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateLicenseManagerSpec checks validity and makes default updates to a LicenseManagerSpec, and returns error if something is wrong.
func validateLicenseManagerSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) error {

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// checkLicenseRelatedPodFailures checks license status via Splunk API
// and publishes warning event when expired license is detected
func checkLicenseRelatedPodFailures(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager, statefulSet *appsv1.StatefulSet) error {
	logger := logging.FromContext(ctx).With("func", "checkLicenseRelatedPodFailures")
	eventPublisher := GetEventPublisher(ctx, cr)

	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}

	for i := int32(0); i < replicas; i++ {
		// Check if pod is ready before attempting API call
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), i)
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		err := client.Get(ctx, namespacedName, &pod)
		if err != nil {
			logger.InfoContext(ctx, "pod not found, skipping license check", "podName", podName)
			continue
		}

		// A Running Pod can still be absent from the headless Service's DNS
		// records. Wait for Kubernetes readiness before calling the management
		// endpoint so normal startup is not reported as a license-health fault.
		if pod.Status.Phase != corev1.PodRunning {
			logger.InfoContext(ctx, "pod not in running state, skipping license check", "podName", podName, "phase", pod.Status.Phase)
			continue
		}
		if !isLicenseManagerPodReady(&pod) {
			logger.InfoContext(ctx, "pod not ready, skipping license check", "podName", podName)
			continue
		}

		// Get admin password from namespace-scoped secret
		defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
		defaultSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), defaultSecretObjName)
		if err != nil {
			return fmt.Errorf("failed to get namespace secret for license check: %w", err)
		}

		adminPassword := string(defaultSecret.Data["password"])
		if adminPassword == "" {
			return fmt.Errorf("admin password not found in secret %s", defaultSecretObjName)
		}

		// Create Splunk client
		fqdnName := GetSplunkStatefulsetURL(cr.GetNamespace(), SplunkLicenseManager, cr.GetName(), i, false)
		splunkClient := newSplunkClientFunc(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPassword)

		// Get license information from Splunk API
		licenses, err := splunkClient.GetLicenseInfo()
		if err != nil {
			logger.ErrorContext(ctx, "failed to get license information from Splunk API", "error", err, "podName", podName)
			eventPublisher.Warning(ctx, EventReasonLicenseHealthCheckFailed,
				fmt.Sprintf("Unable to query license health from Pod '%s'; reconciliation will retry", podName))
			continue
		}

		// Check for expired licenses
		for licenseName, licenseInfo := range licenses {
			if licenseInfo.Status == "EXPIRED" {
				eventPublisher.Warning(ctx, EventReasonLicenseExpired,
					fmt.Sprintf("License '%s' has expired", licenseName))
				logger.ErrorContext(ctx, "detected expired license", "licenseName", licenseName, "title", licenseInfo.Title)
			}
		}
	}

	return nil
}

func isLicenseManagerPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}

// helper function to get the list of LicenseManager types in the current namespace
func getLicenseManagerList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.LicenseManagerList, error) {
	logger := logging.FromContext(ctx).With("func", "getLicenseManagerList", "name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.LicenseManagerList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		logger.ErrorContext(ctx, "LicenseManager types not found in namespace", "error", err, "namespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}
