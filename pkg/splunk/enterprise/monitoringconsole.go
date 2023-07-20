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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyMonitoringConsole reconciles the StatefulSet for N monitoring console instances of Splunk Enterprise.
func ApplyMonitoringConsole(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyMonitoringConsole")
	eventPublisher, _ := newK8EventPublisher(client, cr)

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	// validate and updates defaults for CR
	err := validateMonitoringConsoleSpec(ctx, client, cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterpriseApi.PhaseError

	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
	if err != nil {
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
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-monitoring-console", cr.GetName())

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr)

	// create or update general config resources
	_, err = ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkLicenseManager)
			if err != nil {
				return result, err
			}
		}

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a headless service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create or update headless service failed %s", err.Error()))
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create or update regular service failed %s", err.Error()))
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getMonitoringConsoleStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getMonitoringConsoleStatefulSet", fmt.Sprintf("get monitoring console stateful set failed %s", err.Error()))
		return result, err
	}

	// check if the Monitoring Console is ready for version upgrade, if required
	continueReconcile, err := isMonitoringConsoleReadyForUpgrade(ctx, client, cr)
	if err != nil || !continueReconcile {
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, 1)
	if err != nil {
		eventPublisher.Warning(ctx, "getMonitoringConsoleStatefulSet", fmt.Sprintf("update to default statefuleset pod manager failed %s", err.Error()))
		return result, err
	}
	cr.Status.Phase = phase

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
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, 1, []corev1.EnvVar{})
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
func getMonitoringConsoleList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.MonitoringConsoleList, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getMonitoringConsoleList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.MonitoringConsoleList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		scopedLog.Error(err, "MonitoringConsole types not found in namespace", "namsespace", cr.GetNamespace())
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
	if err != nil && !k8serrors.IsNotFound(err) {
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

// AddURLsConfigMap for adding new server peers to the monitoring console or scaling up
func AddURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar) {
	for _, url := range newURLs {
		_, ok := revised.Data[url.Name]
		if !ok {
			revised.Data[url.Name] = url.Value
		} else {
			newInsURLs := strings.Split(url.Value, ",")
			//1. Find number of URLs, that crname,  present in the current configmap
			var crURLs string
			for _, newURL := range newInsURLs {
				if strings.Contains(revised.Data[url.Name], newURL) {
					if crURLs == "" {
						crURLs = newURL
					} else {
						str := []string{crURLs, newURL}
						crURLs = strings.Join(str, ",")
					}
				}
			}
			//2. if length of both same then just reconcile
			if len(crURLs) == len(url.Value) {
				//reconcile
				break
			} else if len(crURLs) < len(url.Value) { //3. incoming URLs are more than current scaling up
				//scaling UP
				for _, newEntry := range newInsURLs {
					if !strings.Contains(revised.Data[url.Name], newEntry) {
						str := []string{revised.Data[url.Name], newEntry}
						revised.Data[url.Name] = strings.Join(str, ",")
					}
				}
			} else { //4. incoming URLs are less than current then scaling down
				//scaling DOWN pods
				DeleteURLsConfigMap(revised, crName, newURLs, false)
			}
		}
	}
}

// DeleteURLsConfigMap for deleting server peers to the monitoring console or scaling down
func DeleteURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar, deleteCR bool) {
	for _, url := range newURLs {
		currentURLs := strings.Split(revised.Data[url.Name], ",")
		sort.Strings(currentURLs)
		for _, curr := range currentURLs {
			//scale DOWN
			if strings.Contains(curr, crName) && !strings.Contains(url.Value, curr) && !deleteCR {
				revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], curr, "")
			} else if strings.Contains(curr, crName) && deleteCR {
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

// isMonitoringConsoleReadyForUpgrade checks if MonitoringConsole can be upgraded if a version upgrade is in-progress
// No-operation otherwise; returns bool, err accordingly
func isMonitoringConsoleReadyForUpgrade(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isMonitoringConsoleReadyForUpgrade").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	// check if a LicenseManager is attached to the instance
	clusterManagerRef := cr.Spec.ClusterManagerRef
	if clusterManagerRef.Name == "" {
		return true, nil
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetName()),
	}

	// check if the stateful set is created at this instance
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, namespacedName, statefulSet)
	if err != nil && k8serrors.IsNotFound(err) {
		return true, nil
	}

	namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: clusterManagerRef.Name}
	clusterManager := &enterpriseApi.ClusterManager{}

	// get the cluster manager referred in monitoring console
	err = c.Get(ctx, namespacedName, clusterManager)
	if err != nil {
		eventPublisher.Warning(ctx, "isMonitoringConsoleReadyForUpgrade", fmt.Sprintf("Could not find the Cluster Manager. Reason %v", err))
		scopedLog.Error(err, "Unable to get clusterManager")
		return true, err
	}

	cmImage, err := getCurrentImage(ctx, c, cr, SplunkClusterManager)
	if err != nil {
		eventPublisher.Warning(ctx, "isMonitoringConsoleReadyForUpgrade", fmt.Sprintf("Could not get the Cluster Manager Image. Reason %v", err))
		scopedLog.Error(err, "Unable to get clusterManager current image")
		return false, err
	}

	mcImage, err := getCurrentImage(ctx, c, cr, SplunkMonitoringConsole)
	if err != nil {
		eventPublisher.Warning(ctx, "isMonitoringConsolerReadyForUpgrade", fmt.Sprintf("Could not get the Monitoring Console Image. Reason %v", err))
		scopedLog.Error(err, "Unable to get monitoring console current image")
		return false, err
	}

	// check if an image upgrade is happening and whether CM has finished updating yet, return false to stop
	// further reconcile operations on MC until CM is ready
	if (cr.Spec.Image != mcImage) && (clusterManager.Status.Phase != enterpriseApi.PhaseReady || cmImage != cr.Spec.Image) {
		return false, nil
	}

	return true, nil
}

// changeMonitoringConsoleAnnotations updates the splunk/image-tag field of the MonitoringConsole annotations to trigger the reconcile loop
// on update, and returns error if something is wrong.
func changeMonitoringConsoleAnnotations(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("changeMonitoringConsoleAnnotations").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(client, cr)

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
		eventPublisher.Warning(ctx, "changeMonitoringConsoleAnnotations", fmt.Sprintf("Could not get the ClusterManager Image. Reason %v", err))
		scopedLog.Error(err, "Get ClusterManager Image failed with", "error", err)
		return err
	}
	err = changeAnnotations(ctx, client, image, monitoringConsoleInstance)
	if err != nil {
		eventPublisher.Warning(ctx, "changeMonitoringConsoleAnnotations", fmt.Sprintf("Could not update annotations. Reason %v", err))
		scopedLog.Error(err, "MonitoringConsole types update after changing annotations failed with", "error", err)
		return err
	}

	return nil
}
