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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplySearchHeadCluster reconciles the state for a Splunk Enterprise search head cluster.
func ApplySearchHeadCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplySearchHeadCluster")

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "SearchHeadCluster"

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.SearchHeadClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	cr.Status.DeployerPhase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateSearchHeadClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonValidateSpecFailed, fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Search Head Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "Search Head Cluster spec validation failed", err)
	}
	if cr.GetDeletionTimestamp() == nil {
		err = validateSHCDefaultsRestartSafety(ctx, client, cr)
		if err != nil {
			eventPublisher.Warning(
				ctx,
				EventReasonValidateSpecFailed,
				fmt.Sprintf(
					"Search Head Cluster configuration cannot use phased restart for %s — check operator logs",
					cr.GetName(),
				),
			)
			setPhaseAndConditions(
				enterpriseApi.PhaseError,
				"Search Head Cluster configuration requires an unsupported restart mode",
			)
			return reconcile.Result{}, splcommon.NewTerminalError(
				EventReasonValidateSpecFailed,
				"Search Head Cluster configuration requires an unsupported restart mode",
				err,
			)
		}
	}

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed")
		return result, err
	}

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkSearchHead)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonApplySplunkConfigFailed, fmt.Sprintf("Failed to apply general config for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, EventReasonAppFrameworkInitFailed, fmt.Sprintf("App framework initialization failed for %s — check operator logs", cr.GetName()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed")
			return result, err
		}
	}

	// updates status after function completes
	cr.Status.DeployerPhase = enterpriseApi.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-search-head", cr.GetName())
	if cr.Status.Members == nil {
		cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{}
	}
	if cr.Status.ShcSecretChanged == nil {
		cr.Status.ShcSecretChanged = []bool{}
	}
	if cr.Status.AdminSecretChanged == nil {
		cr.Status.AdminSecretChanged = []bool{}
	}
	if cr.Status.AdminPasswordChangedSecrets == nil {
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if searchHeadClusterLifecycleEnabled() {
			cr.Status.LifecycleOperation = shcworkflow.StartClusterDeletion(
				cr.Status.LifecycleOperation,
				cr.GetName(),
				searchHeadClusterLifecycleNow(),
			)
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getSearchHeadEnv(cr), false)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console env ConfigMap during deletion")
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkSearchHead)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up resources during deletion")
				return result, err
			}
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkSearchHead)

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
			cr.Status.DeployerPhase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, EventReasonDeleteFailed, fmt.Sprintf("Failed to delete custom resource %s — check operator logs", cr.GetName()))
		}
		return result, err
	}

	// create or update a headless search head cluster service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, true))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Search Head headless service")
		return result, err
	}

	// create or update a regular search head cluster service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, false))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Search Head service")
		return result, err
	}

	// create or update a deployer service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, false))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Deployer service")
		return result, err
	}

	// create or update statefulset for the deployer
	statefulSet, err := getDeployerStatefulSet(ctx, client, cr)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Deployer StatefulSet")
		return result, err
	}

	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		continueReconcile, err := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if err != nil || !continueReconcile {
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed")
			}
			return result, err
		}
	}

	deployerManager := splctrl.DefaultStatefulSetPodManager{}
	phase, err := deployerManager.Update(ctx, client, statefulSet, 1)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Deployer pods")
		return result, err
	}
	cr.Status.DeployerPhase = phase

	// create or update statefulset for the search heads
	statefulSet, err = getSearchHeadStatefulSet(ctx, client, cr)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update Search Head StatefulSet")
		return result, err
	}
	if searchHeadClusterLifecycleEnabled() {
		if err = applySearchHeadPodDisruptionBudget(ctx, client, cr, statefulSet); err != nil {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply Search Head PodDisruptionBudget")
			return result, err
		}
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, getSearchHeadEnv(cr))
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to validate Monitoring Console reference")
		return result, err
	}

	mgr := newSearchHeadClusterPodManager(client, cr, namespaceScopedSecret, splclient.NewSplunkClient)

	// handle SHC upgrade process
	phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)

	if err != nil {
		message := cr.Status.Message
		if message == "" {
			message = "Failed to update Search Head pods"
		}
		setPhaseAndConditions(enterpriseApi.PhaseError, message)
		if _, terminal := splcommon.TerminalMessage(err); terminal {
			return reconcile.Result{}, err
		}
		return result, err
	}
	setPhaseAndConditions(phase, cr.Status.Message)

	var finalResult *reconcile.Result
	if cr.Status.DeployerPhase == enterpriseApi.PhaseReady {
		finalResult = handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
	}

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getSearchHeadEnv(cr), true)
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

		// Reset secrets related status structs
		cr.Status.ShcSecretChanged = []bool{}
		cr.Status.AdminSecretChanged = []bool{}
		cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := addTelApp(ctx, podExecClient, numberOfDeployerReplicas, cr)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to install Telemetry app")
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}
		// Update the requeue result as needed by the app framework
		if finalResult != nil {
			result = *finalResult
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// validateSHCDefaultsRestartSafety repeats the admission restart-safety
// classification from observed Kubernetes state. The validation webhook is
// optional, so reconciliation must not update the defaults ConfigMap before it
// proves that an existing SHC can consume the change through phased restart.
func validateSHCDefaultsRestartSafety(
	ctx context.Context,
	controllerClient splcommon.ControllerClient,
	cr *enterpriseApi.SearchHeadCluster,
) error {
	previousDefaults := ""
	defaultsConfigMap := &corev1.ConfigMap{}
	defaultsName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      GetSplunkDefaultsName(cr.GetName(), SplunkSearchHead),
	}
	err := controllerClient.Get(ctx, defaultsName, defaultsConfigMap)
	switch {
	case err == nil:
		previousDefaults = defaultsConfigMap.Data["default.yml"]
	case k8serrors.IsNotFound(err):
		// A missing defaults ConfigMap is a create only when the Search Head
		// StatefulSet is also absent. If the StatefulSet exists, classify the
		// requested defaults against an empty prior document.
		currentStatefulSet := &appsv1.StatefulSet{}
		statefulSetName := types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name: GetSplunkStatefulsetName(
				SplunkSearchHead,
				cr.GetName(),
			),
		}
		err = controllerClient.Get(
			ctx,
			statefulSetName,
			currentStatefulSet,
		)
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf(
				"read existing Search Head StatefulSet before classifying inline defaults: %w",
				err,
			)
		}
	default:
		return fmt.Errorf(
			"read current Search Head inline defaults before classification: %w",
			err,
		)
	}

	classification, err := splunkconfig.ClassifySHCDefaultsRestart(
		cr.Spec.Defaults,
		previousDefaults,
	)
	if err != nil {
		return fmt.Errorf(
			"cannot classify inline Search Head Cluster configuration restart safety: %w",
			err,
		)
	}
	if classification.RequiresSimultaneousRestart {
		return fmt.Errorf(
			"changing [shclustering] setting %q requires an approximately simultaneous restart and cannot be treated as an ordinary phased Search Head Cluster rollout",
			classification.Setting,
		)
	}
	return nil
}

// ApplyShcSecret synchronizes the admin password after proving that the
// namespace shc_secret still matches every existing member. An shc_secret
// change modifies [shclustering] and requires an approximately simultaneous
// restart, which this phased controller does not automate.
func ApplyShcSecret(ctx context.Context, mgr *searchHeadClusterPodManager, replicas int32, podExecClient splutil.PodExecClientImpl) error {
	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.cr)

	// Get namespace scoped secret
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mgr.c, mgr.cr.GetNamespace())
	if err != nil {
		return err
	}

	logger := logging.FromContext(ctx).With("func", "ApplyShcSecret", "desiredReplicas", replicas, "shcSecretChanged", mgr.cr.Status.ShcSecretChanged, "adminSecretChanged", mgr.cr.Status.AdminSecretChanged, "crStatusNamespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion, "namespaceSecretResourceVersion", namespaceSecret.GetObjectMeta().GetResourceVersion())

	// If namespace scoped secret revision is the same ignore
	if len(mgr.cr.Status.NamespaceSecretResourceVersion) == 0 {
		// First time, set resource version in CR
		logger.InfoContext(ctx, "setting CrStatusNamespaceSecretResourceVersion for the first time")
		mgr.cr.Status.NamespaceSecretResourceVersion = namespaceSecret.ObjectMeta.ResourceVersion
		return nil
	} else if mgr.cr.Status.NamespaceSecretResourceVersion == namespaceSecret.ObjectMeta.ResourceVersion {
		// If resource version hasn't changed don't return
		return nil
	}

	logger.InfoContext(ctx, "namespaced scoped secret revision has changed")

	// Retrieve shc_secret password from secret data
	nsShcSecret := string(namespaceSecret.Data["shc_secret"])

	// Retrieve shc_secret password from secret data
	nsAdminSecret := string(namespaceSecret.Data["password"])

	type podSecretObservation struct {
		ordinal       int32
		podName       string
		adminPassword string
	}
	observations := make([]podSecretObservation, 0, replicas)

	// Observe every member before changing any credential. This prevents an
	// early admin-password update from running before a later member reveals
	// that the namespace shc_secret was rotated.
	for i := int32(0); i <= replicas-1; i++ {
		shPodName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), i)
		shcSecret, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, shPodName, mgr.cr.GetNamespace(), "shc_secret")
		if err != nil {
			return fmt.Errorf("couldn't retrieve shc_secret from secret data, error: %s", err.Error())
		}
		adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, shPodName, mgr.cr.GetNamespace(), "password")
		if err != nil {
			return fmt.Errorf("couldn't retrieve admin password from secret data, error: %s", err.Error())
		}
		if shcSecret != nsShcSecret {
			const message = "namespace shc_secret rotation is blocked because Search Head Cluster security-key changes require an approximately simultaneous restart that the phased Kubernetes lifecycle does not automate"
			mgr.cr.Status.Message = "SHCSecretRotationBlocked: " + message
			logger.ErrorContext(
				ctx,
				"namespace shc_secret differs from an existing Search Head member",
				"pod", shPodName,
			)
			if eventPublisher != nil {
				eventPublisher.Warning(
					ctx,
					EventReasonSHCSecretRotationBlocked,
					"Namespace shc_secret rotation was not applied; use a supported Search Head Cluster security-key rotation procedure",
				)
			}
			return errors.New(message)
		}
		observations = append(observations, podSecretObservation{
			ordinal:       i,
			podName:       shPodName,
			adminPassword: adminPwd,
		})
	}

	adminPasswordsChanged := 0
	for _, observation := range observations {
		podLogger := logging.FromContext(ctx).With(
			"func", "ApplyShcSecretPodLoop",
			"desiredReplicas", replicas,
			"adminSecretChanged", mgr.cr.Status.AdminSecretChanged,
			"namespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion,
			"pod", observation.podName,
		)
		podExecClient.SetTargetPodName(ctx, observation.podName)
		streamOptions := &remotecommand.StreamOptions{}

		// If admin secret is different from namespace scoped secret change it
		if observation.adminPassword != nsAdminSecret {
			podLogger.InfoContext(ctx, "admin password different from namespace scoped secret, changing admin password")
			// If admin password already changed, ignore
			if observation.ordinal < int32(len(mgr.cr.Status.AdminSecretChanged)) {
				if mgr.cr.Status.AdminSecretChanged[observation.ordinal] {
					continue
				}
			}

			// Change admin password on splunk instance of pod
			command := fmt.Sprintf("/opt/splunk/bin/splunk cmd splunkd rest --noauth POST /services/admin/users/admin 'password=%s'", nsAdminSecret)
			streamOptions.Stdin = strings.NewReader(command)
			_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
			if err != nil {
				return err
			}
			podLogger.InfoContext(ctx, "admin password changed on the splunk instance of pod")

			// Get client for Pod and restart splunk instance on pod
			shClient := mgr.getClient(ctx, observation.ordinal)
			err = shClient.RestartSplunk()
			if err != nil {
				return err
			}
			podLogger.InfoContext(ctx, "restarted Splunk")

			// Set the adminSecretChanged changed flag to true
			if observation.ordinal < int32(len(mgr.cr.Status.AdminSecretChanged)) {
				mgr.cr.Status.AdminSecretChanged[observation.ordinal] = true
			} else {
				podLogger.InfoContext(ctx, "appending to AdminSecretChanged")
				mgr.cr.Status.AdminSecretChanged = append(mgr.cr.Status.AdminSecretChanged, true)
			}
			adminPasswordsChanged++

			// Adding to map of secrets to be synced
			podSecret, err := splutil.GetSecretFromPod(
				ctx,
				mgr.c,
				observation.podName,
				mgr.cr.GetNamespace(),
			)
			if err != nil {
				return err
			}
			mgr.cr.Status.AdminPasswordChangedSecrets[podSecret.GetName()] = true
			podLogger.InfoContext(ctx, "secret mounted on pod(to be changed) added to map")
		}
	}

	/*
		When admin password on the secret mounted on SHC pod is different from that on the namespace scoped
		secret the operator updates the admin password on the Splunk Instance running on the Pod. At this point
		the admin password on the secret mounted on SHC pod is different from the Splunk Instance running on it.
		Since the operator utilizes the admin password retrieved from the secret mounted on a SHC pod to make
		REST API calls to the Splunk instances running on SHC Pods, it results in unsuccessful authentication.
		Update the admin password on secret mounted on SHC pod to ensure successful authentication.
	*/
	if len(mgr.cr.Status.AdminPasswordChangedSecrets) > 0 {

		for podSecretName := range mgr.cr.Status.AdminPasswordChangedSecrets {
			podSecret, err := splutil.GetSecretByName(ctx, mgr.c, mgr.cr.GetNamespace(), podSecretName)
			if err != nil {
				return fmt.Errorf("could not read secret %s, reason - %v", podSecretName, err)
			}
			podSecret.Data["password"] = []byte(nsAdminSecret)
			_, err = splctrl.ApplySecret(ctx, mgr.c, podSecret)
			if err != nil {
				return err
			}
			logger.InfoContext(ctx, "admin password changed on the secret mounted on pod")
		}
	}

	// Emit event for password sync completed
	if eventPublisher != nil {
		eventPublisher.Normal(ctx, EventReasonPasswordSyncCompleted,
			fmt.Sprintf("Password synchronized for %d pods", adminPasswordsChanged))
	}

	return nil
}

// getSearchHeadStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise search heads.
func getSearchHeadStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (*appsv1.StatefulSet, error) {

	certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs))
	if err != nil {
		return nil, err
	}

	// get search head env variables with deployer
	env := getSearchHeadEnv(cr)

	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, cr.Spec.Replicas, env, certMounts)
	if err != nil {
		return nil, err
	}

	updateStrategy, err := getSearchHeadStatefulSetUpdateStrategy(
		ctx,
		client,
		cr,
		&ss.Spec.Template,
	)
	if err != nil {
		return nil, err
	}
	ss.Spec.UpdateStrategy = updateStrategy

	return ss, nil
}

// getSearchHeadStatefulSetUpdateStrategy keeps OnDelete as the compatibility
// default and renders a fail-closed RollingUpdate partition only when the SHC
// lifecycle contract explicitly selects Kubernetes-owned Pod replacement.
func getSearchHeadStatefulSetUpdateStrategy(
	ctx context.Context,
	client splcommon.ControllerClient,
	cr *enterpriseApi.SearchHeadCluster,
	desiredTemplate *corev1.PodTemplateSpec,
) (appsv1.StatefulSetUpdateStrategy, error) {
	onDelete := appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.OnDeleteStatefulSetStrategyType,
	}
	if !searchHeadClusterLifecycleEnabled() {
		return onDelete, nil
	}

	policy, err := ResolveSearchHeadClusterLifecyclePolicy(&cr.Spec)
	if err != nil {
		return appsv1.StatefulSetUpdateStrategy{}, err
	}
	if policy.PodUpdateStrategy != enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate {
		operation := cr.Status.LifecycleOperation
		rollbackPending := operation != nil &&
			operation.Intent == enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
			operation.TargetOrdinal != nil &&
			operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted
		if !rollbackPending {
			return onDelete, nil
		}

		current := &appsv1.StatefulSet{}
		err = client.Get(ctx, types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
		}, current)
		if k8serrors.IsNotFound(err) {
			return onDelete, nil
		}
		if err != nil {
			return appsv1.StatefulSetUpdateStrategy{}, err
		}
		if current.Spec.UpdateStrategy.Type !=
			appsv1.RollingUpdateStatefulSetStrategyType {
			return onDelete, nil
		}
		if current.Spec.Replicas == nil ||
			current.Spec.UpdateStrategy.RollingUpdate == nil ||
			current.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
			return appsv1.StatefulSetUpdateStrategy{}, fmt.Errorf(
				"cannot roll back Search Head StatefulSet %s with an incomplete RollingUpdate strategy",
				current.GetName(),
			)
		}
		partition := *current.Spec.UpdateStrategy.RollingUpdate.Partition
		if partition < 0 || partition > *current.Spec.Replicas {
			return appsv1.StatefulSetUpdateStrategy{}, fmt.Errorf(
				"cannot roll back Search Head StatefulSet %s with partition %d outside replica range 0..%d",
				current.GetName(),
				partition,
				*current.Spec.Replicas,
			)
		}
		if partition > *operation.TargetOrdinal {
			// Kubernetes has not been authorized to replace this target. It is
			// safe to restore OnDelete now; the durable lifecycle operation is
			// retained and the Operator continues that same target.
			return onDelete, nil
		}
		return appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.RollingUpdateStatefulSetStrategyType,
			RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
				Partition: &partition,
			},
		}, nil
	}

	partitionCeiling := cr.Spec.Replicas
	partition := partitionCeiling
	current := &appsv1.StatefulSet{}
	err = client.Get(ctx, types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
	}, current)
	if err != nil && !k8serrors.IsNotFound(err) {
		return appsv1.StatefulSetUpdateStrategy{}, err
	}
	if err == nil && current.Spec.Replicas != nil {
		partitionCeiling = *current.Spec.Replicas
		partition = partitionCeiling
	}
	if err == nil &&
		current.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType &&
		current.Spec.UpdateStrategy.RollingUpdate != nil &&
		current.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		currentPartition := *current.Spec.UpdateStrategy.RollingUpdate.Partition
		currentTemplate := current.Spec.Template.DeepCopy()
		templateChanged := splctrl.MergePodUpdates(
			ctx,
			currentTemplate,
			desiredTemplate,
			current.GetName(),
		)
		if !templateChanged &&
			currentPartition >= 0 &&
			currentPartition <= partitionCeiling {
			partition = currentPartition
		}
	}

	return appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
		RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
			Partition: &partition,
		},
	}, nil
}

// CSPL-3652 Configure deployer resources if configured
// Use default otherwise
// Make sure to set the resources ONLY for the deployer
func setDeployerConfig(ctx context.Context, cr *enterpriseApi.SearchHeadCluster, podTemplate *corev1.PodTemplateSpec) error {
	logger := logging.FromContext(ctx).With("func", "setDeployerConfig", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Break out if this is not a deployer
	if !strings.Contains("deployer", podTemplate.Labels["app.kubernetes.io/name"]) {
		return errors.New("not a deployer, skipping setting resources")
	}
	depRes := cr.Spec.DeployerResourceSpec
	for i := range podTemplate.Spec.Containers {
		if len(depRes.Requests) != 0 {
			podTemplate.Spec.Containers[i].Resources.Requests = cr.Spec.DeployerResourceSpec.Requests
			logger.InfoContext(ctx, "setting deployer resources requests", "requests", cr.Spec.DeployerResourceSpec.Requests)
		}

		if len(depRes.Limits) != 0 {
			podTemplate.Spec.Containers[i].Resources.Limits = cr.Spec.DeployerResourceSpec.Limits
			logger.InfoContext(ctx, "setting deployer resources limits", "limits", cr.Spec.DeployerResourceSpec.Limits)
		}
	}

	// Add node affinity if configured
	if cr.Spec.DeployerNodeAffinity != nil {
		podTemplate.Spec.Affinity.NodeAffinity = cr.Spec.DeployerNodeAffinity
		logger.InfoContext(ctx, "setting deployer node affinity", "nodeAffinity", cr.Spec.DeployerNodeAffinity)
	}

	return nil
}

// getDeployerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getDeployerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs))
	if err != nil {
		return nil, err
	}
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, 1, getSearchHeadExtraEnv(cr, cr.Spec.Replicas), certMounts)
	if err != nil {
		return ss, err
	}

	// CSPL-3562 - Set deployer resources if configured
	err = setDeployerConfig(ctx, cr, &ss.Spec.Template)
	if err != nil {
		return ss, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateSearchHeadClusterSpec checks validity and makes default updates to a SearchHeadClusterSpec, and returns error if something is wrong.
func validateSearchHeadClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster) error {
	if cr.Spec.Replicas < 3 {
		cr.Spec.Replicas = 3
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// helper function to get the list of SearchHeadCluster types in the current namespace
func getSearchHeadClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.SearchHeadClusterList, error) {
	logger := logging.FromContext(ctx).With("func", "getSearchHeadClusterList", "name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.SearchHeadClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		logger.ErrorContext(ctx, "SearchHeadCluster types not found in namespace", "error", err, "namespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}
