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
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// ApplyDeployment creates or updates a Kubernetes Deployment
func ApplyDeployment(ctx context.Context, c splcommon.ControllerClient, revised *appsv1.Deployment) (enterpriseApi.Phase, error) {
	scopedLog := log.WithName("ApplyDeployment").WithValues(
		"name", revised.GetObjectMeta().GetName(),
		"namespace", revised.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current appsv1.Deployment

	err := c.Get(ctx, namespacedName, &current)
	if err != nil && k8serrors.IsNotFound(err) {
		return enterpriseApi.PhasePending, splutil.CreateResource(ctx, c, revised)
	} else if err != nil {
		return enterpriseApi.PhasePending, err
	}

	// found an existing Deployment

	// check for changes in Pod template
	hasUpdates := MergePodUpdates(&current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName())
	desiredReplicas := *revised.Spec.Replicas
	*revised = current // caller expects that object passed represents latest state

	// check for scaling
	if revised.Spec.Replicas != nil {
		if *revised.Spec.Replicas < desiredReplicas {
			scopedLog.Info(fmt.Sprintf("Scaling replicas up to %d", desiredReplicas))
			*revised.Spec.Replicas = desiredReplicas
			return enterpriseApi.PhaseScalingUp, splutil.UpdateResource(ctx, c, revised)
		} else if *revised.Spec.Replicas > desiredReplicas {
			scopedLog.Info(fmt.Sprintf("Scaling replicas down to %d", desiredReplicas))
			*revised.Spec.Replicas = desiredReplicas
			return enterpriseApi.PhaseScalingDown, splutil.UpdateResource(ctx, c, revised)
		}
	}

	// only update if there are material differences, as determined by comparison function
	if hasUpdates {
		return enterpriseApi.PhaseUpdating, splutil.UpdateResource(ctx, c, revised)
	}

	// check if updates are in progress
	if revised.Status.UpdatedReplicas < revised.Status.Replicas {
		scopedLog.Info("Waiting for updates to complete")
		return enterpriseApi.PhaseUpdating, nil
	}

	// check if replicas are not yet ready
	if revised.Status.ReadyReplicas < desiredReplicas {
		scopedLog.Info("Waiting for pods to become ready")
		if revised.Status.ReadyReplicas > 0 {
			return enterpriseApi.PhaseScalingUp, nil
		}
		return enterpriseApi.PhasePending, nil
	}

	// all is good!
	scopedLog.Info("All pods are ready")
	return enterpriseApi.PhaseReady, nil
}
