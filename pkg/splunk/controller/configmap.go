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

package controller

import (
	"context"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"

	//k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplyConfigMap creates or updates a Kubernetes ConfigMap
func ApplyConfigMap(ctx context.Context, client splcommon.ControllerClient, configMap *corev1.ConfigMap) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyConfigMap").WithValues(
		"name", configMap.GetObjectMeta().GetName(),
		"namespace", configMap.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: configMap.GetNamespace(), Name: configMap.GetName()}
	var current corev1.ConfigMap

	err := client.Get(context.TODO(), namespacedName, &current)

	// dataUpdated flag returns if the data on the configMap has been succesfully updated
	dataUpdated := false
	if err == nil {
		// updateNeeded flag indicates if an update to the configMap is necessary
		updateNeeded := false

		// dataDifferent flag indicates the data on the configmap read from etcd is different
		// from the data on the configMap passed as an argument to this function
		dataDifferent := false
		if !reflect.DeepEqual(configMap.Data, current.Data) {
			scopedLog.Info("Updating existing ConfigMap", "ResourceVerison", current.GetResourceVersion())
			current.Data = configMap.Data
			updateNeeded = true
			dataDifferent = true
			configMap = &current
		}
		if !reflect.DeepEqual(configMap.GetOwnerReferences(), current.GetOwnerReferences()) {
			scopedLog.Info("Updating existing ConfigMap", "ResourceVerison", current.GetResourceVersion())
			current.OwnerReferences = configMap.OwnerReferences
			updateNeeded = true
			configMap = &current
		}

		if updateNeeded {
			err = splutil.UpdateResource(ctx, client, &current)
			if err == nil {
				configMap = &current
				// Update the dataUpdated flag only when there is a data change
				// and configMap is successfully updated by client
				if dataDifferent {
					dataUpdated = true
				}
			}
		} else {
			scopedLog.Info("No changes for ConfigMap")
		}

	} else if errors.IsNotFound(err) {
		err = splutil.CreateResource(ctx, client, configMap)
		if err == nil {
			dataUpdated = true
			gerr := client.Get(ctx, namespacedName, &current)
			for ; gerr != nil; gerr = client.Get(ctx, namespacedName, &current) {
				scopedLog.Error(gerr, "Newly created resource still not in cache sleeping for 10 micro second", "configmap", configMap.Name, "error", gerr.Error())
				time.Sleep(10 * time.Microsecond)
			}
			configMap = &current
		}
	}

	return dataUpdated, err
}

// GetConfigMap gets the ConfigMap resource in a given namespace
func GetConfigMap(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName) (*corev1.ConfigMap, error) {
	var configMap corev1.ConfigMap
	err := client.Get(ctx, namespacedName, &configMap)
	if err != nil {
		return nil, err
	}
	return &configMap, nil
}

// GetConfigMapResourceVersion gets the Resource version of a configMap
func GetConfigMapResourceVersion(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName) (string, error) {
	configMap, err := GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		return "", err
	}
	return configMap.ResourceVersion, nil
}

// GetMCConfigMap gets the MC ConfigMap resource required for that MC
func GetMCConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) (*corev1.ConfigMap, error) {
	var configMap corev1.ConfigMap
	err := client.Get(ctx, namespacedName, &configMap)
	if err != nil && errors.IsNotFound(err) {
		//if we don't find mc configmap create and return an empty configmap
		configMap = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespacedName.Name,
				Namespace: namespacedName.Namespace,
			},
			Data: make(map[string]string),
		}
		err = splutil.CreateResource(ctx, client, &configMap)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	err = SetConfigMapOwnerRef(ctx, client, cr, namespacedName)
	if err != nil {
		return nil, err
	}
	err = client.Get(ctx, namespacedName, &configMap)
	if err != nil {
		return nil, err
	}
	err = client.Get(context.TODO(), namespacedName, &configMap)
	if err != nil {
		return nil, err
	}
	return &configMap, nil
}

// SetConfigMapOwnerRef sets owner references for configMap
func SetConfigMapOwnerRef(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) error {
	configMap, err := GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		return err
	}

	currentOwnerRef := configMap.GetOwnerReferences()
	// Check if owner ref exists
	for i := 0; i < len(currentOwnerRef); i++ {
		if reflect.DeepEqual(currentOwnerRef[i].UID, cr.GetUID()) {
			return nil
		}
	}

	configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	return splutil.UpdateResource(ctx, client, configMap)
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
