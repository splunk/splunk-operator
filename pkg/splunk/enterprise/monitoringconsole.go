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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyMonitoringConsole reconciles the deployment for N standalone instances of Splunk Enterprise.
func ApplyMonitoringConsole(client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterprisev1.CommonSplunkSpec, extraEnv []corev1.EnvVar) error {
	var secrets *corev1.Secret
	var err error

	secrets, err = GetLatestVersionedSecret(client, cr, cr.GetNamespace(), GetSplunkMonitoringConsoleDeploymentName(SplunkMonitoringConsole, cr.GetNamespace()))
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

	addNewURLs := true

	if cr.GetObjectMeta().GetDeletionTimestamp() != nil {
		addNewURLs = false
	}

	//_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), extraEnv, addNewURLs)
	_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), extraEnv, addNewURLs)
	if err != nil {
		return err
	}
	//_, err = ApplyMonitoringConsoleEnvConfigMap(client, cr.GetNamespace(), cr.GetName(), extraEnv, addNewURLs)
	//configMapHash to trigger the configMap change in monitoring console deployment
	configMapHash, err := getConfigMapHash(client, cr.GetNamespace())
	if err != nil {
		return err
	}

	deploymentMC, err := getMonitoringConsoleDeployment(cr, &spec, SplunkMonitoringConsole, 1, configMapHash, secretName)
	if err != nil {
		return err
	}

	_, err = splctrl.ApplyDeployment(client, deploymentMC)
	if err != nil {
		return err
	}

	return err
}

// GetMonitoringConsoleDeployment returns a Kubernetes Deployment object for Splunk Enterprise monitoring console instance.
func getMonitoringConsoleDeployment(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, replicas int32, configMapHash string, secretName string) (*appsv1.Deployment, error) {
	ports := splcommon.SortContainerPorts(getSplunkContainerPorts(SplunkMonitoringConsole))
	annotations := splcommon.GetIstioAnnotations(ports)
	var partOfIdentifier string
	selectLabels := getSplunkLabels(cr.GetNamespace(), instanceType, partOfIdentifier) //using Namespace here so that with every CR the name should remain same Ex- splunk-<namespace>-monitoring-console
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

	if configMapHash == "" {
		return nil, nil
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
//func ApplyMonitoringConsoleEnvConfigMap(client splcommon.ControllerClient, namespace string, newURLs []corev1.EnvVar, addNewURLs bool) (*corev1.ConfigMap, error) {
func ApplyMonitoringConsoleEnvConfigMap(client splcommon.ControllerClient, namespace string, crName string, newURLs []corev1.EnvVar, addNewURLs bool) (*corev1.ConfigMap, error) {

	var current corev1.ConfigMap
	current.Data = make(map[string]string)

	//1. Retrieve the existing McExtraEnvMap contents
	namespacedName := types.NamespacedName{Namespace: namespace, Name: GetSplunkMonitoringconsoleConfigMapName(namespace, SplunkMonitoringConsole)}
	err := client.Get(context.TODO(), namespacedName, &current) //save existing in current

	//If no configMap and deletion of CR is requested then retrun nil
	if err != nil && addNewURLs == false {
		//create a configMap with no values
		current = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetSplunkMonitoringconsoleConfigMapName(namespace, SplunkMonitoringConsole),
				Namespace: namespace,
			},
			Data: make(map[string]string),
		}

		err = splctrl.CreateResource(client, &current)
		if err != nil {
			return nil, err
		}
		return &current, nil
	}

	if err == nil {
		revised := current.DeepCopy()
		for _, url := range newURLs {
			_, ok := revised.Data[url.Name]
			if addNewURLs { //if new instances are coming up
				if !ok { //key doesn't exist in the configMap, then add it
					revised.Data[url.Name] = url.Value //if new url doesn't exist add it in configmap
				} else {
					//TODO: For "SEARCH_HEAD_URL" need to find a better approach to update configMap, any other data structure?
					if url.Name == "SPLUNK_SEARCH_HEAD_URL" {
						if revised.Data[url.Name] == url.Value {
							break
						}
						//delete all URLs for the crName custom resource
						currentURLs := strings.Split(revised.Data[url.Name], ",") //split the existing values
						for _, curr := range currentURLs {
							if strings.Contains(curr, crName) { //delete that entry
								revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], curr, "")
								if strings.HasPrefix(revised.Data[url.Name], ",") {
									res := revised.Data[url.Name]
									revised.Data[url.Name] = strings.TrimPrefix(res, ",")
								}
								if strings.HasSuffix(revised.Data[url.Name], ",") {
									res := revised.Data[url.Name]
									revised.Data[url.Name] = strings.TrimSuffix(res, ",")
								}
								if strings.Contains(revised.Data[url.Name], ",,") {
									res := revised.Data[url.Name]
									revised.Data[url.Name] = strings.ReplaceAll(res, ",,", ",")
								}
							}
						}
						newInsURLs := strings.Split(url.Value, ",")
						for _, newEntry := range newInsURLs {
							if !(strings.Contains(revised.Data[url.Name], newEntry)) {
								if revised.Data[url.Name] == "" {
									revised.Data[url.Name] = newEntry
								} else {
									str := []string{revised.Data[url.Name], newEntry}
									revised.Data[url.Name] = strings.Join(str, ",")
								}
							}
						}
					} else {
						//now add the incoming URLs as fresh entry
						newInsURL := strings.Split(url.Value, ",")
						for _, newEntry := range newInsURL {
							if !(strings.Contains(revised.Data[url.Name], newEntry)) {
								str := []string{revised.Data[url.Name], newEntry}
								revised.Data[url.Name] = strings.Join(str, ",")
							}
						}
					}
				}
			} else { //removing custom resource. Update configMap
				//check if only dummy entery exist then no deletion can happen
				if strings.Contains(revised.Data[url.Name], url.Value) {
					revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], url.Value, "")
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
		if !reflect.DeepEqual(revised.Data, current.Data) {
			current.Data = revised.Data
			err = splctrl.UpdateResource(client, &current)
			if err != nil {
				return nil, err
			}
		}
		return &current, nil
	}

	for _, url := range newURLs {
		current.Data[url.Name] = url.Value
	}

	// Set name and namespace
	current.ObjectMeta = metav1.ObjectMeta{
		Name:      GetSplunkMonitoringconsoleConfigMapName(namespace, SplunkMonitoringConsole),
		Namespace: namespace,
	}

	err = splctrl.CreateResource(client, &current)
	if err != nil {
		return nil, err
	}

	return &current, nil
}

func getConfigMapHash(client splcommon.ControllerClient, namespace string) (string, error) {

	namespacedName := types.NamespacedName{Namespace: namespace, Name: GetSplunkMonitoringconsoleConfigMapName(namespace, SplunkMonitoringConsole)}
	var current corev1.ConfigMap

	err := client.Get(context.TODO(), namespacedName, &current)
	// if configMap doens't exist and we try to get configMap hash just return with empty string
	if err != nil {
		return "", err
	}

	contents := current.Data //map[string]string

	hash := md5.New()
	b := new(bytes.Buffer)

	//check key sorting. Collect keys in array and sort. Then use that sorted keys to append
	var keys []string

	for k := range contents {
		keys = append(keys, k)
	}

	sort.Strings(keys) //sort keys here

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
