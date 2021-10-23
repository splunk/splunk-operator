// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyMonitoringConsole reconciles the StatefulSet for N monitoring console instances of Splunk Enterprise.
func ApplyMonitoringConsole(client splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplyMonitoringConsole").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	// validate and updates defaults for CR
	err := validateMonitoringConsoleSpec(cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := initAndCheckAppInfoStatus(client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			cr.Status.AppContext.IsDeploymentInProgress = false
			return result, err
		}
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-monitoring-console", cr.GetName())
	defer func() {
		client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole)
	if err != nil {
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := splctrl.CheckForDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a headless service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, true))
	if err != nil {
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getMonitoringConsoleStatefulSet(client, cr)
	if err != nil {
		return result, err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		if cr.Status.AppContext.AppsSrcDeployStatus != nil {
			markAppsStatusToComplete(client, cr, &cr.Spec.AppFrameworkConfig, cr.Status.AppContext.AppsSrcDeployStatus)
			// Schedule one more reconcile in next 5 seconds, just to cover any latest app framework config changes
			if cr.Status.AppContext.IsDeploymentInProgress {
				cr.Status.AppContext.IsDeploymentInProgress = false
				return result, nil
			}
		}

		// Requeue the reconcile after polling interval if we had set the lastAppInfoCheckTime.
		if cr.Status.AppContext.LastAppInfoCheckTime != 0 {
			result.RequeueAfter = GetNextRequeueTime(cr.Status.AppContext.AppsRepoStatusPollInterval, cr.Status.AppContext.LastAppInfoCheckTime)
		} else {
			result.Requeue = false
		}
	}
	return result, nil
}

// getMonitoringConsoleStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise monitoring console instances.
func getMonitoringConsoleStatefulSet(client splcommon.ControllerClient, cr *enterpriseApi.MonitoringConsole) (*appsv1.StatefulSet, error) {
	// get generic statefulset for Splunk Enterprise objects
	var monitoringConsoleConfigMap *corev1.ConfigMap
	configMap := GetSplunkMonitoringconsoleConfigMapName(cr.GetName(), SplunkMonitoringConsole)
	ss, err := getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, 1, []corev1.EnvVar{})
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
	monitoringConsoleConfigMap, err = splctrl.GetMCConfigMap(client, cr, namespacedName)
	if err != nil {
		return nil, err
	}
	ss.Spec.Template.ObjectMeta.Annotations[monitoringConsoleConfigRev] = monitoringConsoleConfigMap.ResourceVersion

	// Setup App framework init containers
	setupAppInitContainers(client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)
	return ss, nil
}

// validateMonitoringConsoleSpec checks validity and makes default updates to a MonitoringConsole, and returns error if something is wrong.
func validateMonitoringConsoleSpec(cr *enterpriseApi.MonitoringConsole) error {
	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(&cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true)
		if err != nil {
			return err
		}
	}
	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

//ApplyMonitoringConsoleEnvConfigMap creates or updates a Kubernetes ConfigMap for extra env for monitoring console pod
func ApplyMonitoringConsoleEnvConfigMap(client splcommon.ControllerClient, namespace string, crName string, monitoringConsoleRef string, newURLs []corev1.EnvVar, addNewURLs bool) (*corev1.ConfigMap, error) {

	var current corev1.ConfigMap
	current.Data = make(map[string]string)

	configMap := GetSplunkMonitoringconsoleConfigMapName(monitoringConsoleRef, SplunkMonitoringConsole)
	namespacedName := types.NamespacedName{Namespace: namespace, Name: configMap}
	err := client.Get(context.TODO(), namespacedName, &current)

	if err == nil {
		revised := current.DeepCopy()
		if addNewURLs {
			AddURLsConfigMap(revised, crName, newURLs)
		} else {
			DeleteURLsConfigMap(revised, crName, newURLs, true)
		}
		if !reflect.DeepEqual(revised.Data, current.Data) {
			current.Data = revised.Data
			err = splutil.UpdateResource(client, &current)
			if err != nil {
				return nil, err
			}
		}
		return &current, nil
	}

	//If no configMap and deletion of CR is requested then create a empty configMap
	if err != nil && addNewURLs == false {
		current = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMap,
				Namespace: namespace,
			},
			Data: make(map[string]string),
		}
	} else {
		//else create a new configMap with new entries
		for _, url := range newURLs {
			current.Data[url.Name] = url.Value
		}
	}

	current.ObjectMeta = metav1.ObjectMeta{
		Name:      configMap,
		Namespace: namespace,
	}

	err = splutil.CreateResource(client, &current)
	if err != nil {
		return nil, err
	}

	return &current, nil
}

//AddURLsConfigMap for adding new server peers to the monitoring console or scaling up
func AddURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar) {
	for _, url := range newURLs {
		_, ok := revised.Data[url.Name]
		if !ok {
			revised.Data[url.Name] = url.Value
		} else {
			newInsURLs := strings.Split(url.Value, ",")
			currentURLs := strings.Split(revised.Data[url.Name], ",")
			var crURLs string
			for _, curr := range currentURLs {
				if strings.Contains(curr, crName) {
					if crURLs == "" {
						crURLs = curr
					} else {
						str := []string{curr, crURLs}
						crURLs = strings.Join(str, ",")
					}
				}
			}
			if len(crURLs) == len(url.Value) {
				//reconcile
				break
			} else if len(crURLs) < len(url.Value) {
				//scaling UP
				for _, newEntry := range newInsURLs {
					if !strings.Contains(revised.Data[url.Name], newEntry) {
						str := []string{revised.Data[url.Name], newEntry}
						revised.Data[url.Name] = strings.Join(str, ",")
					}
				}
			} else {
				//scaling DOWN pods
				DeleteURLsConfigMap(revised, crName, newURLs, false)
			}
		}
	}
}

//DeleteURLsConfigMap for deleting server peers to the monitoring console or scaling down
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
			//if deleting "SPLUNK_MULTISITE_MANAGER" delete "SPLUNK_SITE"
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
