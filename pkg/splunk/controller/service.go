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

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplyService creates or updates a Kubernetes Service
func ApplyService(ctx context.Context, client splcommon.ControllerClient, revised *corev1.Service) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyService").WithValues(
		"name", revised.GetObjectMeta().GetName(),
		"namespace", revised.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current corev1.Service

	err := client.Get(ctx, namespacedName, &current)
	if err != nil && k8serrors.IsNotFound(err) {
		return splutil.CreateResource(ctx, client, revised)
	} else if err != nil {
		return err
	}

	// check for changes in service template
	hasUpdates := MergeServiceSpecUpdates(ctx, &current.Spec, &revised.Spec, current.GetObjectMeta().GetName())
	*revised = current // caller expects that object passed represents latest state

	// only update if there are material differences, as determined by comparison function
	if hasUpdates {
		scopedLog.Info("Updating existing Service")
		err = splutil.UpdateResource(ctx, client, revised)
		if err != nil {
			return err
		}
		err = client.Get(ctx, namespacedName, revised)
		if err != nil {
			return err
		}
	}

	// all is good!
	scopedLog.Info("No update to existing Service")
	return nil
}
