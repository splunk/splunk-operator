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

package deploy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyService creates or updates a Kubernetes Service
func ApplyService(client ControllerClient, service *corev1.Service) error {
	scopedLog := log.WithName("ApplyService").WithValues(
		"name", service.GetObjectMeta().GetName(),
		"namespace", service.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: service.GetNamespace(), Name: service.GetName()}
	var current corev1.Service

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing Service: do nothing
		scopedLog.Info("Found existing Service")
	} else {
		err = CreateResource(client, service)
	}

	return err
}
