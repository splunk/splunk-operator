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

package clustermanager

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"log/slog"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/reconcile/upgrade"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
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
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// apply owns the request-level ClusterManager reconciliation boundary.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "ClusterManager", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	instance := &enterpriseApi.ClusterManager{}
	if err := client.Get(ctx, namespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not load cluster manager data: %w", err)
	}
	if instance.GetAnnotations()[enterpriseApi.ClusterManagerPausedAnnotation] == "true" {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{Phase: instance.Status.Phase, IsPaused: true, Generation: instance.GetGeneration()})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update paused status", "error", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true, RequeueAfter: splcommon.PauseRetryDelay}, nil
	} else if condition := meta.FindStatusCondition(instance.Status.Conditions, string(enterpriseApi.ConditionPaused)); condition != nil && condition.Status == metav1.ConditionTrue {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{Phase: instance.Status.Phase, IsPaused: false, Generation: instance.GetGeneration()})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update unpaused status", "error", err)
			return reconcile.Result{}, err
		}
	}
	logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)
	result, err := ApplyClusterManager(ctx, client, instance, nil)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}
	fresh := &enterpriseApi.ClusterManager{}
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
	if _, terminal := splcommon.TerminalMessage(err); terminal {
		return reconcile.Result{}, err
	}
	return result, err
}

// Apply is the request-level entry point used by the controller.
var Apply = apply

// ApplyClusterManager reconciles the ClusterManager resource after the request boundary has loaded it.
var ApplyClusterManager = applyClusterManager

// Apply reconciles the state of a Splunk Enterprise cluster manager.
// podExecClient parameter is optional - if nil, a real PodExecClient will be created.
// This allows tests to inject a mock client.
func applyClusterManager(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	var err error
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplyClusterManager")

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "ClusterManager"

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.ClusterManagerPausedAnnotation] == "true"
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
	err = validateClusterManagerSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonValidateSpecFailed, fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Cluster Manager spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "Cluster Manager spec validation failed", err)
	}

	// updates status after function completes
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-%s", cr.GetName(), "cluster-manager")

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		k8sops.AreRemoteVolumeKeysChanged(ctx, client, cr, splcommon.SplunkClusterManager, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		if err != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonRemoteVolumeKeyCheckFailed, fmt.Sprintf("Remote volume key change check failed for %s — check operator logs", cr.GetName()))
			setPhaseAndConditions(enterpriseApi.PhaseError, "SmartStore remote volume key validation failed")
			return result, err
		}

		_, configMapDataChanged, err := k8sops.ApplySmartstoreConfigMap(ctx, client, cr, &cr.Spec.SmartStore)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply SmartStore ConfigMap")
			return result, err
		} else if configMapDataChanged {
			// Do not auto populate with configMapDataChanged flag to NeedToPushManagerApps. Set it only  if
			// configMapDataChanged it true. It mush be reset, only upon initiating the bundle push REST call,
			// once the CM is in ready state otherwise, we keep retrying
			cr.Status.BundlePushTracker.NeedToPushManagerApps = true
			cr.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix()
		}

		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	// This is to take care of case where AreRemoteVolumeKeysChanged returns an error if it returns false.
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "SmartStore remote volume key validation failed")
		return result, err
	}

	// If needed, Migrate the app framework status
	err = appframework.CheckAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
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
			eventPublisher.Warning(ctx, splcommon.EventReasonAppFrameworkInitFailed, fmt.Sprintf("App framework initialization failed for %s — check operator logs", cr.GetName()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	// create or update general config resources
	namespaceScopedSecret, err := k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonApplySplunkConfigFailed, fmt.Sprintf("Failed to apply general config for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// Smart Store secrets get created manually and should not be managed by the Operator
	if &cr.Spec.SmartStore != nil {
		_ = k8sops.DeleteOwnerReferencesForS3SecretObjects(ctx, client, cr, &cr.Spec.SmartStore)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			extraEnv, _ := getCMMultisiteEnvVars(ctx, cr, namespaceScopedSecret)
			_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, false)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap during deletion")
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = appframework.UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, splcommon.SplunkClusterManager)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up app framework ConfigMap during deletion")
				return result, err
			}
		}

		// Check if ClusterManager has any remaining references to other CRs, if so don't delete
		err = checkClusterManagerRemainingReferences(ctx, client, cr)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Cluster Manager still has remaining CR references")
			return result, err
		}

		_ = k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkClusterManager)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)

		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonDeleteFailed, fmt.Sprintf("Failed to delete custom resource %s — check operator logs", cr.GetName()))
		}
		return result, err
	}

	// create or update a regular service for the cluster manager
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkClusterManager, false))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update service")
		return result, err
	}

	// create or update statefulset for the cluster manager
	statefulSet, err := getClusterManagerStatefulSet(ctx, client, cr)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	extraEnv, _ := getCMMultisiteEnvVars(ctx, cr, namespaceScopedSecret)
	err = k8sops.ValidateMonitoringConsoleRef(ctx, client, statefulSet, extraEnv)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		// check if the ClusterManager is ready for version upgrade, if required
		continueReconcile, err := upgrade.UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if err != nil || !continueReconcile {
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed")
			} else {
				// waiting on a dependency (e.g. LicenseManager recycling) is not an error,
				// so don't leave the earlier-staged PhaseError as the persisted status
				setPhaseAndConditions(enterpriseApi.PhasePending, "Waiting for upgrade path dependency to become ready")
			}
			return result, err
		}
	}

	clusterManagerManager := k8sops.DefaultStatefulSetPodManager{}
	phase, err := clusterManagerManager.Update(ctx, client, statefulSet, 1)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods")
		return result, err
	}
	setPhaseAndConditions(phase, "")

	//Update MC configmap
	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, true)
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

		// Create podExecClient (use injected one if provided, otherwise create real one)
		if podExecClient == nil {
			podExecClient = splutil.GetPodExecClient(client, cr, "")
		}

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			err := telapp.AddTelApp(ctx, podExecClient, 1, cr)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to install Telemetry app")
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}

		// Manager apps bundle push requires multiple reconcile iterations in order to reflect the configMap on the CM pod.
		// So keep PerformCmBundlePush() as the last call in this block of code, so that other functionalities are not blocked
		err = performCmBundlePush(ctx, client, cr, podExecClient)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to push Manager Apps bundle")
			return result, err
		}

		finalResult := appframework.HandleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

		// trigger MonitoringConsole reconcile by changing the splunk/image-tag annotation
		err = changeMonitoringConsoleAnnotations(ctx, client, cr)
		if err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to trigger Monitoring Console reconciliation")
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

// clusterManagerPodManager is used to manage the cluster manager pod
type clusterManagerPodManager struct {
	log             *slog.Logger
	cr              *enterpriseApi.ClusterManager
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// getClusterManagerClient for clusterManagerPodManager returns a SplunkClient for cluster manager
func (mgr *clusterManagerPodManager) getClusterManagerClient(cr *enterpriseApi.ClusterManager) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), splcommon.GetSplunkServiceName(splcommon.SplunkClusterManager, cr.GetName(), false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// validateClusterManagerSpec checks validity and makes default updates to a ClusterManagerSpec, and returns error if something is wrong.
func validateClusterManagerSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		err := reconcileutil.ValidateSplunkSmartstoreSpec(ctx, &cr.Spec.SmartStore)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := appframework.ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return reconcileutil.ValidateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// getClusterManagerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise cluster manager.
func getClusterManagerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) (*appsv1.StatefulSet, error) {
	var extraEnvVar []corev1.EnvVar

	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNames(splcommon.SplunkClusterManager, cr.GetName(), cr.GetNamespace(), 1)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	ss, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkClusterManager, 1, extraEnvVar)
	if err != nil {
		return ss, err
	}
	certs.InjectCertMounts(&ss.Spec.Template, certMounts)
	smartStoreConfigMap := k8sops.GetSmartstoreConfigMap(ctx, client, cr, splcommon.SplunkClusterManager)

	if smartStoreConfigMap != nil {
		resources.SetupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, splcommon.CommandForClusterManagerSmartstore, cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.EphemeralStorage)
	}
	// Setup App framework staging volume for apps
	resources.SetupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// getCMMultisiteEnvVars checks if cluster is multisite and returns appropriate environment variables
// If it fails to connect to the cluster manager (e.g., pod not ready yet), it returns basic env vars as fallback
// The indirection lets unit tests replace the remote probe.
var getCMMultisiteEnvVars = func(ctx context.Context, cr *enterpriseApi.ClusterManager, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
	logger := logging.FromContext(ctx).With("func", "GetCMMultisiteEnvVars", "name", cr.GetName(), "namespace", cr.GetNamespace())

	extraEnv := resources.GetClusterManagerExtraEnv(cr)

	mgr := clusterManagerPodManager{log: logger, cr: cr, secrets: namespaceScopedSecret, newSplunkClient: splclient.NewSplunkClient}
	cm := mgr.getClusterManagerClient(cr)
	clusterInfo, err := cm.GetClusterInfo(cr.Spec.CommonSplunkSpec.Mock)
	if err != nil {
		logger.ErrorContext(ctx, "failed to get cluster info from ClusterManager pod, using basic environment variables", "error", err)
		return extraEnv, err
	}

	if clusterInfo != nil && clusterInfo.MultiSite == "true" {
		extraEnv = append(extraEnv,
			corev1.EnvVar{Name: "SPLUNK_SITE", Value: "site0"},
			corev1.EnvVar{Name: "SPLUNK_MULTISITE_MASTER", Value: splcommon.GetSplunkServiceName(splcommon.SplunkClusterManager, cr.GetName(), false)})
	}

	return extraEnv, nil
}

// checkClusterManagerRemainingReferences prevents deletion while another CR still points at this manager.
func checkClusterManagerRemainingReferences(ctx context.Context, c splcommon.ControllerClient, cmCr splcommon.MetaObject) error {
	scopedLog := logging.FromContext(ctx).With("func", "CheckClusterManagerRemainingReferences", "cmCr", cmCr.GetName(), "namespace", cmCr.GetNamespace())
	listOpts := []rclient.ListOption{rclient.InNamespace(cmCr.GetNamespace())}
	idxcList, err := k8sops.GetIndexerClusterList(ctx, c, cmCr, listOpts)
	if err != nil {
		if !strings.Contains(err.Error(), "NotFound") && !apierrors.IsNotFound(err) {
			scopedLog.ErrorContext(ctx, "couldn't retrieve IndexerCluster list", "error", err)
			return err
		}
	}
	for _, item := range idxcList.Items {
		if item.Spec.ClusterManagerRef.Name == cmCr.GetName() {
			scopedLog.ErrorContext(ctx, fmt.Sprintf(`IndexerCluster %s still has a reference for ClusterManager %s,
				please backup if needed and delete the IndexerCluster`, item.GetName(), cmCr.GetName()))
			return fmt.Errorf("ClusterManager has stale references to an indexerCluster")
		}
	}
	shcList, err := k8sops.GetSearchHeadClusterList(ctx, c, cmCr, listOpts)
	if err != nil {
		if !strings.Contains(err.Error(), "NotFound") && !apierrors.IsNotFound(err) {
			scopedLog.ErrorContext(ctx, "couldn't retrieve SearchHeadCluster list", "error", err)
			return err
		}
	}
	for _, item := range shcList.Items {
		if item.Spec.ClusterManagerRef.Name == cmCr.GetName() {
			scopedLog.ErrorContext(ctx, fmt.Sprintf(`SearchHeadCluster %s still has a reference for ClusterManager %s,
				please backup if needed and delete the SearchHeadCluster`, item.GetName(), cmCr.GetName()))
			return fmt.Errorf("ClusterManager has stale references to a searchHeadCluster")
		}
	}
	lmList, err := k8sops.GetLicenseManagerList(ctx, c, cmCr, listOpts)
	if err != nil {
		if !strings.Contains(err.Error(), "NotFound") && !apierrors.IsNotFound(err) {
			scopedLog.ErrorContext(ctx, "couldn't retrieve LicenseManager list", "error", err)
			return err
		}
	}
	for _, item := range lmList.Items {
		if item.Spec.ClusterManagerRef.Name == cmCr.GetName() {
			scopedLog.ErrorContext(ctx, fmt.Sprintf(`LicenseManager %s still has a reference for ClusterManager %s,
				please backup if needed and delete the LicenseManager`, item.GetName(), cmCr.GetName()))
			return fmt.Errorf("ClusterManager has stale references to a LicenseManager")
		}
	}
	mcList, err := k8sops.GetMonitoringConsoleList(ctx, c, cmCr, listOpts)
	if err != nil {
		if !strings.Contains(err.Error(), "NotFound") && !apierrors.IsNotFound(err) {
			scopedLog.ErrorContext(ctx, "couldn't retrieve MonitoringConsole list", "error", err)
			return err
		}
	}
	for _, item := range mcList.Items {
		if item.Spec.ClusterManagerRef.Name == cmCr.GetName() {
			scopedLog.ErrorContext(ctx, fmt.Sprintf(`MonitoringConsole %s still has a reference for ClusterManager %s,
				please backup if needed and delete the MonitoringConsole`, item.GetName(), cmCr.GetName()))
			return fmt.Errorf("ClusterManager has stale references to a MonitoringConsole")
		}
	}
	return nil
}

// changeMonitoringConsoleAnnotations causes MonitoringConsole resources that use this manager to reconcile.
func changeMonitoringConsoleAnnotations(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {
	logger := logging.FromContext(ctx).With("func", "changeMonitoringConsoleAnnotations", "name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	monitoringConsoleInstance := &enterpriseApi.MonitoringConsole{}
	if cr.Spec.MonitoringConsoleRef.Name != "" {
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.MonitoringConsoleRef.Name}
		var err error
		monitoringConsoleInstance, err = k8sops.GetMonitoringConsole(ctx, c, cr, namespacedName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
	} else {
		listOpts := []rclient.ListOption{rclient.InNamespace(cr.GetNamespace())}
		monitoringConsoleList, err := k8sops.GetMonitoringConsoleList(ctx, c, cr, listOpts)
		if err != nil {
			if apierrors.IsNotFound(err) || err.Error() == "NotFound" {
				return nil
			}
			return err
		}
		for i := range monitoringConsoleList.Items {
			if monitoringConsoleList.Items[i].Spec.ClusterManagerRef.Name == cr.GetName() {
				monitoringConsoleInstance = &monitoringConsoleList.Items[i]
				break
			}
		}
		if monitoringConsoleInstance.GetName() == "" {
			return nil
		}
	}

	statefulSetImage, err := k8sops.GetStatefulSetImage(ctx, c, cr, splcommon.SplunkClusterManager)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonAnnotationUpdateFailed, fmt.Sprintf("Could not get the ClusterManager Image. Reason %v", err))
		logger.ErrorContext(ctx, "get ClusterManager Image failed with", "error", err)
		return err
	}
	if err = k8sops.ChangeAnnotations(ctx, c, statefulSetImage, monitoringConsoleInstance); err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonAnnotationUpdateFailed, fmt.Sprintf("Could not update annotations. Reason %v", err))
		logger.ErrorContext(ctx, "MonitoringConsole types update after changing annotations failed with", "error", err)
		return err
	}
	return nil
}
