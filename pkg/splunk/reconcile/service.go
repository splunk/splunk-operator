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

package reconcile

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyService creates or updates a Kubernetes Service
func ApplyService(client ControllerClient, revised *corev1.Service) error {
	scopedLog := rconcilelog.WithName("ApplyService").WithValues(
		"name", revised.GetObjectMeta().GetName(),
		"namespace", revised.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current corev1.Service

	err := client.Get(context.TODO(), namespacedName, &current)
	if err != nil {
		return CreateResource(client, revised)
	}

	// check for changes in service template
	hasUpdates := MergeServiceSpecUpdates(&current.Spec, &revised.Spec, current.GetObjectMeta().GetName())
	*revised = current // caller expects that object passed represents latest state

	// only update if there are material differences, as determined by comparison function
	if hasUpdates {
		scopedLog.Info("Updating existing Service")
		return UpdateResource(client, revised)
	}

	// all is good!
	scopedLog.Info("No update to existing Service")
	return nil
}
