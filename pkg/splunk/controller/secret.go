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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// ApplySecret creates or updates a Kubernetes Secret, and returns active secrets if successful
func ApplySecret(client splcommon.ControllerClient, secret *corev1.Secret) (*corev1.Secret, error) {
	scopedLog := log.WithName("ApplySecret").WithValues(
		"name", secret.GetObjectMeta().GetName(),
		"namespace", secret.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: secret.GetNamespace(), Name: secret.GetName()}
	var current corev1.Secret
	result := &current

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		scopedLog.Info("Updating existing Secret")
		err = UpdateResource(client, secret)
	} else {
		scopedLog.Info("Creating a new Secret")
		err = CreateResource(client, secret)
	}

	result = secret

	return result, err
}
