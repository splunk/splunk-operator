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

package controller

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// ApplyConfigMap creates or updates a Kubernetes ConfigMap
func ApplyConfigMap(client splcommon.ControllerClient, configMap *corev1.ConfigMap) (bool, error) {
	scopedLog := log.WithName("ApplyConfigMap").WithValues(
		"name", configMap.GetObjectMeta().GetName(),
		"namespace", configMap.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: configMap.GetNamespace(), Name: configMap.GetName()}
	var current corev1.ConfigMap

	err := client.Get(context.TODO(), namespacedName, &current)
	var dataUpdated bool
	if err == nil {
		if !reflect.DeepEqual(configMap.Data, current.Data) {
			scopedLog.Info("Updating existing ConfigMap")
			current.Data = configMap.Data
			err = splutil.UpdateResource(client, &current)
			if err == nil {
				dataUpdated = true
			}
		} else {
			scopedLog.Info("No changes for ConfigMap")
		}
	} else {
		err = splutil.CreateResource(client, configMap)
		if err == nil {
			dataUpdated = true
		}
	}

	return dataUpdated, err
}

// GetConfigMap gets the ConfigMap resource in a given namespace
func GetConfigMap(client splcommon.ControllerClient, namespacedName types.NamespacedName) (*corev1.ConfigMap, error) {
	var configMap corev1.ConfigMap
	err := client.Get(context.TODO(), namespacedName, &configMap)
	if err != nil {
		return nil, err
	}
	return &configMap, nil
}

// GetConfigMapResourceVersion gets the Resource version of a configMap
func GetConfigMapResourceVersion(client splcommon.ControllerClient, namespacedName types.NamespacedName) (string, error) {
	configMap, err := GetConfigMap(client, namespacedName)
	if err != nil {
		return "", err
	}
	return configMap.ResourceVersion, nil
}

// GetMCConfigMap gets the MC ConfigMap resource required for that MC
func GetMCConfigMap(client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) (*corev1.ConfigMap, error) {
	var configMap corev1.ConfigMap
	err := client.Get(context.TODO(), namespacedName, &configMap)
	if err != nil {
		//if we don't find mc configmap create and return an empty configmap
		var configMap corev1.ConfigMap
		configMap = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespacedName.Name,
				Namespace: namespacedName.Namespace,
			},
			Data: make(map[string]string),
		}
		err = splutil.CreateResource(client, &configMap)
		if err != nil {
			return nil, err
		}
	}
	err = SetConfigMapOwnerRef(client, cr, namespacedName)
	if err != nil {
		return nil, err
	}
	return &configMap, nil
}

// SetConfigMapOwnerRef sets owner references for configMap
func SetConfigMapOwnerRef(client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) error {
	configMap, err := GetConfigMap(client, namespacedName)
	if err != nil {
		return err
	}

	currentOwnerRef := configMap.GetOwnerReferences()
	// Check if owner ref exists
	for i := 0; i < len(currentOwnerRef); i++ {
		if reflect.DeepEqual(currentOwnerRef[i], splcommon.AsOwner(cr, false)) {
			return nil
		}
	}

	// Owner ref doesn't exist, update configmap with owner references
	configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	return splutil.UpdateResource(client, configMap)
}

// PrepareConfigMap prepares and returns a K8 ConfigMap object for the given data
func PrepareConfigMap(configMapName, namespace string, dataMap map[string]string) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
	}
	configMap.Data = dataMap

	return configMap
}
