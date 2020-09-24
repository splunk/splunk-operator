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

package controller

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// ApplyConfigMap creates or updates a Kubernetes ConfigMap
func ApplyConfigMap(client splcommon.ControllerClient, configMap *corev1.ConfigMap) error {
	scopedLog := log.WithName("ApplyConfigMap").WithValues(
		"name", configMap.GetObjectMeta().GetName(),
		"namespace", configMap.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: configMap.GetNamespace(), Name: configMap.GetName()}
	var current corev1.ConfigMap

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		if !reflect.DeepEqual(configMap.Data, current.Data) {
			scopedLog.Info("Updating existing ConfigMap")
			current.Data = configMap.Data
			err = UpdateResource(client, &current)
		} else {
			scopedLog.Info("No changes for ConfigMap")
		}
	} else {
		err = CreateResource(client, configMap)
	}

	return err
}

// CheckAndGetIfConfigMapExists returns true if a configMap with the given name exists, else returns false
func CheckAndGetIfConfigMapExists(client splcommon.ControllerClient, namespacedName types.NamespacedName) (bool, corev1.ConfigMap) {
	var confMap corev1.ConfigMap

	return client.Get(context.TODO(), namespacedName, &confMap) == nil, confMap
}
