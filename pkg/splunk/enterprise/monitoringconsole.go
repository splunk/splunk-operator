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
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyMonitoringConsole creates the deployment for monitoring console deployment of Splunk Enterprise.
func ApplyMonitoringConsole(client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterprisev1.CommonSplunkSpec, extraEnv []corev1.EnvVar) error {
	var secrets *corev1.Secret
	var err error

	secrets, err = splutil.GetLatestVersionedSecret(client, cr, cr.GetNamespace(), GetSplunkMonitoringConsoleDeploymentName(SplunkMonitoringConsole, cr.GetNamespace()))
	if err != nil {
		return err
	}

	secretName := ""
	if secrets != nil {
		secretName = secrets.GetName()
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

	_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), extraEnv, addNewURLs)
	if err != nil {
		return err
	}

	//configMapHash to trigger the configMap change in monitoring console deployment
	configMapHash, err := getConfigMapHash(client, cr.GetNamespace())
	if err != nil {
		return err
	}

	//configMapHash should never be nil, just adding extra check
	if configMapHash == "" {
		return err
	}

	deploymentMC, err := getMonitoringConsoleDeployment(cr, &spec, SplunkMonitoringConsole, configMapHash, secretName)
	if err != nil {
		return err
	}

	_, err = splctrl.ApplyDeployment(client, deploymentMC)

	return err
}

// GetMonitoringConsoleDeployment returns a Kubernetes Deployment object for Splunk Enterprise monitoring console instance.
func getMonitoringConsoleDeployment(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, configMapHash string, secretName string) (*appsv1.Deployment, error) {
	var partOfIdentifier string
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

	//create deployment configuration
	deploymentMC := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkMonitoringConsoleDeploymentName(instanceType, cr.GetNamespace()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selectLabels,
			},
			Replicas: &replicas,
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

	env := []corev1.EnvVar{
		{Name: "SPLUNK_CONFIGMAP_HASH",
			Value: configMapHash,
		},
	}
	// append labels and annotations from parent
	splcommon.AppendParentMeta(deploymentMC.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())

	// update statefulset's pod template with common splunk pod config
	updateSplunkPodTemplateWithConfig(&deploymentMC.Spec.Template, cr, spec, instanceType, env, secretName)

	return deploymentMC, nil
}

//ApplyMonitoringConsoleEnvConfigMap creates or updates a Kubernetes ConfigMap for extra env for monitoring console pod
func ApplyMonitoringConsoleEnvConfigMap(client splcommon.ControllerClient, namespace string, crName string, newURLs []corev1.EnvVar, addNewURLs bool) (*corev1.ConfigMap, error) {

	var current corev1.ConfigMap
	current.Data = make(map[string]string)

	configMap := GetSplunkMonitoringconsoleConfigMapName(namespace, SplunkMonitoringConsole)
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

func getConfigMapHash(client splcommon.ControllerClient, namespace string) (string, error) {

	namespacedName := types.NamespacedName{Namespace: namespace, Name: GetSplunkMonitoringconsoleConfigMapName(namespace, SplunkMonitoringConsole)}
	var current corev1.ConfigMap

	err := client.Get(context.TODO(), namespacedName, &current)
	// if configMap doens't exist and we try to get configMap hash just return with empty string
	if err != nil {
		return "", err
	}

	contents := current.Data

	hash := md5.New()
	b := new(bytes.Buffer)

	//check key sorting. Collect keys in array and sort. Then use that sorted keys to append
	var keys []string

	for k := range contents {
		keys = append(keys, k)
	}

	//sort keys here
	sort.Strings(keys)

	//now iterate through the map in sorted order
	for _, k := range keys {
		fmt.Fprintf(b, "%s=\"%s\"\n", k, contents[k])
	}

	if _, err := io.Copy(hash, b); err != nil {
		return "", err
	}

	hashInBytes := hash.Sum(nil)[:16]
	returnMD5String := hex.EncodeToString(hashInBytes)
	return returnMD5String, nil
}
