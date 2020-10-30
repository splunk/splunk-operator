// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyMonitoringConsole creates the statefulset for monitoring console statefulset of Splunk Enterprise.
func ApplyMonitoringConsole(client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterprisev1.CommonSplunkSpec, extraEnv []corev1.EnvVar) error {
	var deletedPeers []string
	secrets, err := splutil.GetLatestVersionedSecret(client, cr, cr.GetNamespace(), GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace()))
	if err != nil {
		return err
	}

	secretName := ""
	if secrets != nil {
		secretName = secrets.GetName()
	}

	//For IndexerCluster custom resource click "Apply changes" on MC and return
	if cr.GetObjectKind().GroupVersionKind().Kind == "IndexerCluster" {
		mgr := monitoringConsolePodManager{cr: &cr, spec: &spec, secrets: secrets, newSplunkClient: splclient.NewSplunkClient}
		c := mgr.getMonitoringConsoleClient(cr)
		err := c.AutomateMCApplyChanges(spec.Mock)
		return err
	}

	// create or update a regular monitoring console service
	err = splctrl.ApplyService(client, getSplunkService(cr, &spec, SplunkMonitoringConsole, false))
	if err != nil {
		return err
	}

	// create or update a headless monitoring console service
	err = splctrl.ApplyService(client, getSplunkService(cr, &spec, SplunkMonitoringConsole, true))
	if err != nil {
		return err
	}

	//by default assume we are adding new instances in the monitoring console configMap
	addNewURLs := true

	if cr.GetObjectMeta().GetDeletionTimestamp() != nil {
		addNewURLs = false
	}

	//get cluster info from cluster master
	if cr.GetObjectKind().GroupVersionKind().Kind == "ClusterMaster" && !spec.Mock {
		mgr := monitoringConsolePodManager{cr: &cr, spec: &spec, secrets: secrets, newSplunkClient: splclient.NewSplunkClient}
		c := mgr.getClusterMasterClient(cr)
		clusterInfo, err := c.GetClusterInfo(spec.Mock)
		if err != nil {
			return err
		}
		multiSite := clusterInfo.MultiSite
		if multiSite == "true" {
			extraEnv = append(extraEnv, corev1.EnvVar{Name: "SPLUNK_SITE", Value: "site0"}, corev1.EnvVar{Name: "SPLUNK_MULTISITE_MASTER", Value: GetSplunkServiceName(SplunkClusterMaster, cr.GetName(), false)})
		}
	}

	_, deletedPeers, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), extraEnv, addNewURLs)
	if err != nil {
		return err
	}

	//set owner reference for splunk monitoring console configMap
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkMonitoringconsoleConfigMapName(cr.GetNamespace(), SplunkMonitoringConsole)}
	err = splctrl.SetConfigMapOwnerRef(client, cr, namespacedName)
	if err != nil {
		return err
	}

	//check what peers are deleted and update distsearch.conf on monitoring console pod
	if len(deletedPeers) > 0 {
		mgr := monitoringConsolePodManager{cr: &cr, spec: &spec, secrets: secrets, newSplunkClient: splclient.NewSplunkClient}
		c := mgr.getMonitoringConsoleClient(cr)
		for i := 0; i < len(deletedPeers); i++ {
			err = c.RemoveSearchPeers(deletedPeers[i], spec.Mock)
			if err != nil {
				return err
			}
		}
	}

	statefulset, err := getMonitoringConsoleStatefulSet(client, cr, &spec, SplunkMonitoringConsole, secretName)
	if err != nil {
		return err
	}

	mgr := splctrl.DefaultStatefulSetPodManager{}
	_, err = mgr.Update(client, statefulset, 1)
	if err != nil {
		return err
	}

	//set owner reference for splunk monitoring console statefulset
	namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
	err = splctrl.SetStatefulSetOwnerRef(client, cr, namespacedName)

	return err
}

// getMonitoringConsoleClient for monitoringConsolePodManager returns a SplunkClient for monitoring console
func (mgr *monitoringConsolePodManager) getMonitoringConsoleClient(cr splcommon.MetaObject) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkMonitoringConsole, cr.GetNamespace(), false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// getClusterMasterClient for monitoringConsolePodManager returns a SplunkClient for cluster master
func (mgr *monitoringConsolePodManager) getClusterMasterClient(cr splcommon.MetaObject) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkClusterMaster, cr.GetName(), false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// monitoringConsolePodManager is used to manage the monitoring console pod
type monitoringConsolePodManager struct {
	cr              *splcommon.MetaObject
	spec            *enterprisev1.CommonSplunkSpec
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// getMonitoringConsoleStatefulSet returns a Kubernetes Statefulset object for Splunk Enterprise monitoring console instance.
func getMonitoringConsoleStatefulSet(client splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, secretName string) (*appsv1.StatefulSet, error) {
	var partOfIdentifier string
	var monitoringConsoleConfigMap *corev1.ConfigMap
	// there will be always 1 replica of monitoring console
	replicas := int32(1)
	ports := splcommon.SortContainerPorts(getSplunkContainerPorts(SplunkMonitoringConsole))
	annotations := splcommon.GetIstioAnnotations(ports)
	//using Namespace here so that with every CR the name should remain same Ex- splunk-<namespace>-monitoring-console
	selectLabels := getSplunkLabels(cr.GetNamespace(), instanceType, partOfIdentifier)
	affinity := splcommon.AppendPodAntiAffinity(&spec.Affinity, cr.GetNamespace(), instanceType.ToString())
	configMap := GetSplunkMonitoringconsoleConfigMapName(cr.GetNamespace(), SplunkMonitoringConsole)

	// start with same labels as selector; note that this object gets modified by splcommon.AppendParentMeta()
	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	//create statefulset configuration
	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(instanceType, cr.GetNamespace()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selectLabels,
			},
			ServiceName:         GetSplunkServiceName(instanceType, cr.GetNamespace(), true),
			Replicas:            &replicas,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Affinity:      affinity,
					Tolerations:   spec.Tolerations,
					SchedulerName: spec.SchedulerName,
					Containers: []corev1.Container{
						{
							Image:           spec.Image,
							ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
							Name:            "splunk",
							Ports:           ports,
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: configMap, //monitoring console env variables configMap
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// prepare and append persistent volume claims if storage is not ephemeral
	var err error
	statefulSet.Spec.VolumeClaimTemplates, err = getSplunkVolumeClaims(cr, spec, labels)
	if err != nil {
		return nil, err
	}

	// add volume mounts to splunk container for the PVCs
	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{
			Name:      "pvc-etc",
			MountPath: "/opt/splunk/etc",
		},
		{
			Name:      "pvc-var",
			MountPath: "/opt/splunk/var",
		},
	}

	env := []corev1.EnvVar{}

	// append labels and annotations from parent
	splcommon.AppendParentMeta(statefulSet.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())

	// update statefulset's pod template with common splunk pod config
	updateSplunkPodTemplateWithConfig(client, &statefulSet.Spec.Template, cr, spec, instanceType, env, secretName)

	//update podTemplate annotation with configMap resource version
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMap}
	monitoringConsoleConfigMap, err = splctrl.GetConfigMap(client, namespacedName)
	if err != nil {
		return nil, err
	}
	statefulSet.Spec.Template.ObjectMeta.Annotations[monitoringConsoleConfigRev] = monitoringConsoleConfigMap.ResourceVersion

	return statefulSet, nil
}

//ApplyMonitoringConsoleEnvConfigMap creates or updates a Kubernetes ConfigMap for extra env for monitoring console pod
func ApplyMonitoringConsoleEnvConfigMap(client splcommon.ControllerClient, namespace string, crName string, newURLs []corev1.EnvVar, addNewURLs bool) (*corev1.ConfigMap, []string, error) {

	var current corev1.ConfigMap
	var deletedPeers []string
	current.Data = make(map[string]string)

	configMap := GetSplunkMonitoringconsoleConfigMapName(namespace, SplunkMonitoringConsole)
	namespacedName := types.NamespacedName{Namespace: namespace, Name: configMap}
	err := client.Get(context.TODO(), namespacedName, &current)

	if err == nil {
		revised := current.DeepCopy()
		if addNewURLs {
			AddURLsConfigMap(revised, crName, newURLs)
		} else {
			deletedPeers = DeleteURLsConfigMap(revised, crName, newURLs, true)
		}
		if !reflect.DeepEqual(revised.Data, current.Data) {
			current.Data = revised.Data
			err = splutil.UpdateResource(client, &current)
			if err != nil {
				return nil, nil, err
			}
		}
		return &current, deletedPeers, nil
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
		return nil, nil, err
	}

	return &current, deletedPeers, nil
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
func DeleteURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar, deleteCR bool) []string {
	var deletedPeers []string
	for _, url := range newURLs {
		currentURLs := strings.Split(revised.Data[url.Name], ",")
		sort.Strings(currentURLs)
		for _, curr := range currentURLs {
			if strings.Contains(curr, crName) {
				if deleteCR {
					revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], url.Value, "")
					if url.Name != "SPLUNK_MULTISITE_MASTER" && url.Name != "SPLUNK_DEPLOYER_URL" && url.Name != "SPLUNK_SEARCH_HEAD_CAPTAIN_URL" && url.Name != "SPLUNK_SITE" {
						deletedPeersTemp := strings.Split(url.Value, ",")
						for i := 0; i < len(deletedPeersTemp); i++ {
							deletedPeers = append(deletedPeers, deletedPeersTemp[i])
						}
					}
				} else if !strings.Contains(url.Value, curr) {
					revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], curr, "")
					deletedPeers = append(deletedPeers, curr)
				}
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
	return deletedPeers
}
