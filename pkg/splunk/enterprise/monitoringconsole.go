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
	"sort"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyMonitoringConsole reconciles the StatefulSet for N monitoring console instances of Splunk Enterprise.
func ApplyMonitoringConsole(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "MonitoringConsole"

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.MonitoringConsolePausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string, isStalled bool) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(), IsStalled: isStalled,
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "", false)

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateMonitoringConsoleSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonValidateSpecFailed, fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Monitoring Console spec validation failed", true)
		return reconcile.Result{}, reconcile.TerminalError(err)
	}

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "App framework migration failed", false)
		return result, err
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, EventReasonAppFrameworkInitFailed, fmt.Sprintf("App framework initialization failed for %s — check operator logs", cr.GetName()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			setPhaseAndConditions(enterpriseApi.PhaseError, "App framework initialization failed", false)
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-monitoring-console", cr.GetName())

	// create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonApplySplunkConfigFailed, fmt.Sprintf("Failed to apply general config for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration", false)
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkLicenseManager)
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to clean up resources during deletion", false)
				return result, err
			}
		}

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted", false)
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a headless service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, true))
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonApplyServiceFailed, fmt.Sprintf("Failed to apply headless service for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service", false)
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, false))
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonApplyServiceFailed, fmt.Sprintf("Failed to apply regular service for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update regular service", false)
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getMonitoringConsoleStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonStatefulSetFailed, fmt.Sprintf("Failed to get monitoring console statefulset for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet", false)
		return result, err
	}

	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		// check if the Monitoring Console is ready for version upgrade, if required
		continueReconcile, err := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, nil)
		if err != nil || !continueReconcile {
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed", false)
			}
			return result, err
		}
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, 1)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonStatefulSetUpdateFailed, fmt.Sprintf("Failed to update statefulset for %s — check operator logs", cr.GetName()))
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods", false)
		return result, err
	}
	setPhaseAndConditions(phase, "", false)

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		finalResult := handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult

	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}
	return result, nil
}

// getMonitoringConsoleStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise monitoring console instances.
func getMonitoringConsoleStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (*appsv1.StatefulSet, error) {
	// get generic statefulset for Splunk Enterprise objects
	var monitoringConsoleConfigMap *corev1.ConfigMap
	configMap := GetSplunkMonitoringconsoleConfigMapName(cr.GetName(), SplunkMonitoringConsole)
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs))
	if err != nil {
		return nil, err
	}
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, 1, []corev1.EnvVar{}, certMounts)
	if err != nil {
		return nil, err
	}
	//use mc configmap as EnvFrom source
	ss.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMap, //monitoring console env variables configMap
				},
			},
		},
	}

	//update podTemplate annotation with configMap resource version
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMap}
	monitoringConsoleConfigMap, err = splctrl.GetMCConfigMap(ctx, client, cr, namespacedName)
	if err != nil {
		return nil, err
	}
	ss.Spec.Template.ObjectMeta.Annotations[monitoringConsoleConfigRev] = monitoringConsoleConfigMap.ResourceVersion

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)
	return ss, nil
}

// helper function to get the list of MonitoringConsole types in the current namespace
func getMonitoringConsoleList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []rclient.ListOption) (enterpriseApi.MonitoringConsoleList, error) {
	logger := logging.FromContext(ctx).With("func", "getMonitoringConsoleList", "name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.MonitoringConsoleList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		logger.ErrorContext(ctx, "MonitoringConsole types not found in namespace", "error", err, "namespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}

// validateMonitoringConsoleSpec checks validity and makes default updates to a MonitoringConsole, and returns error if something is wrong.
func validateMonitoringConsoleSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) error {
	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}
	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// ApplyMonitoringConsoleEnvConfigMap creates or updates a Kubernetes ConfigMap for extra env for monitoring console pod
func ApplyMonitoringConsoleEnvConfigMap(ctx context.Context, client splcommon.ControllerClient, namespace string, crName string, monitoringConsoleRef string, newURLs []corev1.EnvVar, addNewURLs bool) (*corev1.ConfigMap, error) {

	var current corev1.ConfigMap

	configMap := GetSplunkMonitoringconsoleConfigMapName(monitoringConsoleRef, SplunkMonitoringConsole)
	namespacedName := types.NamespacedName{Namespace: namespace, Name: configMap}
	err := client.Get(ctx, namespacedName, &current)

	if err == nil {
		revised := current.DeepCopy()
		if revised.Data == nil {
			revised.Data = make(map[string]string)
		}
		if addNewURLs {
			AddURLsConfigMap(revised, crName, newURLs)
		} else {
			DeleteURLsConfigMap(revised, crName, newURLs, true)
		}
		if !reflect.DeepEqual(revised.Data, current.Data) {
			current.Data = revised.Data
			err = splutil.UpdateResource(ctx, client, &current)
			if err != nil {
				return nil, err
			}
		}
		return &current, nil
	}

	// if err is not resource not found then return the err
	if !k8serrors.IsNotFound(err) {
		return nil, err
	}

	// case when resource not found
	//If no configMap and deletion of CR is requested then create a empty configMap
	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMap,
			Namespace: namespace,
		},
		Data: make(map[string]string),
	}
	if addNewURLs {

		//else create a new configMap with new entries
		for _, url := range newURLs {
			current.Data[url.Name] = url.Value
		}
	}

	current.ObjectMeta = metav1.ObjectMeta{
		Name:      configMap,
		Namespace: namespace,
	}

	err = splutil.CreateResource(ctx, client, &current)
	if err != nil {
		return nil, err
	}

	return &current, nil
}

// crPodNamePrefix derives the per-CR resource-name prefix "splunk-<id>-<kind>-"
// from the first entry of a comma-separated MC URL value. Supports statefulset
// pod URLs (suffix "-<digits>") and service URLs (suffix "-service"/"-headless").
// Returns "" when the prefix cannot be derived; callers then fall back to crName.
func crPodNamePrefix(value string) string {
	if value == "" {
		return ""
	}
	// Pod/service name has no '.', strip any DNS suffix and trailing entries.
	name := strings.SplitN(strings.SplitN(value, ",", 2)[0], ".", 2)[0]
	idx := strings.LastIndex(name, "-")
	if idx <= 0 || idx == len(name)-1 {
		return ""
	}
	suffix := name[idx+1:]
	if suffix != "service" && suffix != "headless" {
		for _, r := range suffix {
			if r < '0' || r > '9' {
				return ""
			}
		}
	}
	return name[:idx+1]
}

// crOwnsURL reports whether `curr` belongs to the CR identified by crPrefix.
// Ownership requires the derived prefix of `curr` to equal crPrefix: a plain
// substring check is unsafe when one CR's name (or kind segment) is contained
// in another's (e.g. "search-head" vs "search-head-adhoc", or "cm" vs
// "cm-cluster-manager-extra"). Falls back to a crName substring match when no
// prefix can be derived.
func crOwnsURL(curr, crPrefix, crName string) bool {
	if crPrefix == "" {
		return strings.Contains(curr, crName)
	}
	if currPrefix := crPodNamePrefix(curr); currPrefix != "" {
		return currPrefix == crPrefix
	}
	return strings.Contains(curr, crPrefix)
}

// AddURLsConfigMap for adding new server peers to the monitoring console or scaling up
func AddURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar) {
	for _, url := range newURLs {
		_, ok := revised.Data[url.Name]
		if !ok {
			revised.Data[url.Name] = url.Value
		} else {
			newInsURLs := strings.Split(url.Value, ",")
			crPrefix := crPodNamePrefix(url.Value)
			// 1. Count CR-owned URLs currently present in the configmap for this key.
			//    We compare counts (not string lengths) because string-length comparison
			//    is unreliable: it depends on whether new entries are a subset of current,
			//    and could never detect scale-down (where current has MORE CR URLs than new).
			currentURLs := strings.Split(revised.Data[url.Name], ",")
			currentCRCount := 0
			for _, curr := range currentURLs {
				if crOwnsURL(curr, crPrefix, crName) {
					currentCRCount++
				}
			}
			newCount := len(newInsURLs)

			// 2. Same count: ensure all new entries are present (otherwise it's a rename/no-op),
			//    nothing to add or remove.
			if currentCRCount == newCount {
				allPresent := true
				for _, newEntry := range newInsURLs {
					if !strings.Contains(revised.Data[url.Name], newEntry) {
						allPresent = false
						break
					}
				}
				if allPresent {
					continue
				}
			}

			if currentCRCount < newCount { // 3. scaling UP
				for _, newEntry := range newInsURLs {
					if !strings.Contains(revised.Data[url.Name], newEntry) {
						str := []string{revised.Data[url.Name], newEntry}
						revised.Data[url.Name] = strings.Join(str, ",")
					}
				}
			} else { // 4. scaling DOWN (currentCRCount > newCount)
				DeleteURLsConfigMap(revised, crName, newURLs, false)
			}
		}
	}
}

// DeleteURLsConfigMap for deleting server peers to the monitoring console or scaling down
func DeleteURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar, deleteCR bool) {
	for _, url := range newURLs {
		crPrefix := crPodNamePrefix(url.Value)
		currentURLs := strings.Split(revised.Data[url.Name], ",")
		sort.Strings(currentURLs)
		for _, curr := range currentURLs {
			//scale DOWN
			if crOwnsURL(curr, crPrefix, crName) && !strings.Contains(url.Value, curr) && !deleteCR {
				revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], curr, "")
			} else if crOwnsURL(curr, crPrefix, crName) && deleteCR {
				revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], url.Value, "")
			}
			//if deleting "SPLUNK_MULTISITE_MASTER" delete "SPLUNK_SITE"
			if url.Name == "SPLUNK_SITE" && deleteCR {
				delete(revised.Data, "SPLUNK_SITE")
			}
			if strings.HasPrefix(revised.Data[url.Name], ",") {
				str := revised.Data[url.Name]
				revised.Data[url.Name] = strings.TrimPrefix(str, ",")
			}
			if strings.HasSuffix(revised.Data[url.Name], ",") {
				str := revised.Data[url.Name]
				revised.Data[url.Name] = strings.TrimSuffix(str, ",")
			}
			if strings.Contains(revised.Data[url.Name], ",,") {
				str := revised.Data[url.Name]
				revised.Data[url.Name] = strings.ReplaceAll(str, ",,", ",")
			}
			if revised.Data[url.Name] == "" {
				delete(revised.Data, url.Name)
			}
		}
	}
}

// changeMonitoringConsoleAnnotations updates the splunk/image-tag field of the MonitoringConsole annotations to trigger the reconcile loop
// on update, and returns error if something is wrong.
func changeMonitoringConsoleAnnotations(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {
	logger := logging.FromContext(ctx).With("func", "changeMonitoringConsoleAnnotations", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, cr)

	monitoringConsoleInstance := &enterpriseApi.MonitoringConsole{}
	if len(cr.Spec.MonitoringConsoleRef.Name) > 0 {
		// if the ClusterManager holds the MonitoringConsoleRef
		namespacedName := types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      cr.Spec.MonitoringConsoleRef.Name,
		}
		err := client.Get(ctx, namespacedName, monitoringConsoleInstance)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
	} else {
		// List out all the MonitoringConsole instances in the namespace
		opts := []rclient.ListOption{
			rclient.InNamespace(cr.GetNamespace()),
		}
		objectList := enterpriseApi.MonitoringConsoleList{}
		err := client.List(ctx, &objectList, opts...)
		if err != nil {
			if err.Error() == "NotFound" {
				return nil
			}
			return err
		}
		if len(objectList.Items) == 0 {
			return nil
		}

		// check if instance has the required ClusterManagerRef
		for _, mc := range objectList.Items {
			if mc.Spec.ClusterManagerRef.Name == cr.GetName() {
				monitoringConsoleInstance = &mc
				break
			}
		}

		if len(monitoringConsoleInstance.GetName()) == 0 {
			return nil
		}
	}

	image, err := getCurrentImage(ctx, client, cr, SplunkClusterManager)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonAnnotationUpdateFailed, fmt.Sprintf("Could not get the ClusterManager Image. Reason %v", err))
		logger.ErrorContext(ctx, "get ClusterManager Image failed with", "error", err)
		return err
	}
	err = changeAnnotations(ctx, client, image, monitoringConsoleInstance)
	if err != nil {
		eventPublisher.Warning(ctx, EventReasonAnnotationUpdateFailed, fmt.Sprintf("Could not update annotations. Reason %v", err))
		logger.ErrorContext(ctx, "MonitoringConsole types update after changing annotations failed with", "error", err)
		return err
	}

	return nil
}
