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
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// ApplyServiceAccount creates or updates a Kubernetes serviceAccount
func ApplyServiceAccount(client splcommon.ControllerClient, serviceAccount *corev1.ServiceAccount) error {
	namespacedName := types.NamespacedName{Namespace: serviceAccount.GetNamespace(), Name: serviceAccount.GetName()}
	var current corev1.ServiceAccount

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		if !reflect.DeepEqual(serviceAccount, &current) {
			current = *serviceAccount
			err = splutil.UpdateResource(client, &current)
		}
	} else {
		err = splutil.CreateResource(client, serviceAccount)
	}

	return err
}

// GetServiceAccount gets the serviceAccount resource in a given namespace
func GetServiceAccount(client splcommon.ControllerClient, namespacedName types.NamespacedName) (*corev1.ServiceAccount, error) {
	scopedLog := log.WithName("GetServiceAccount").WithValues("serviceAccount", namespacedName.Name,
		"namespace", namespacedName.Namespace)
	var serviceAccount corev1.ServiceAccount
	err := client.Get(context.TODO(), namespacedName, &serviceAccount)
	if err != nil {
		scopedLog.Info("ServiceAccount not found")
		return nil, err
	}
	return &serviceAccount, nil
}
