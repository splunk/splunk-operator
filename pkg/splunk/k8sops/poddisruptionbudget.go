// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package k8sops

import (
	"context"
	"fmt"
	"maps"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	policyv1 "k8s.io/api/policy/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyPodDisruptionBudget creates or updates an operator-owned PodDisruptionBudget.
func ApplyPodDisruptionBudget(ctx context.Context, client splcommon.ControllerClient, revised *policyv1.PodDisruptionBudget) error {
	logger := logging.FromContext(ctx).With(
		"func", "ApplyPodDisruptionBudget",
		"name", revised.Name,
		"namespace", revised.Namespace,
	)

	current := &policyv1.PodDisruptionBudget{}
	err := client.Get(ctx, types.NamespacedName{Name: revised.Name, Namespace: revised.Namespace}, current)
	if k8serrors.IsNotFound(err) {
		return splutil.CreateResource(ctx, client, revised)
	}
	if err != nil {
		return err
	}

	if !sameController(current, revised) {
		return fmt.Errorf("PodDisruptionBudget %s/%s is not controlled by the desired owner", current.Namespace, current.Name)
	}

	updated := current.DeepCopy()
	updated.Spec = revised.Spec
	updated.OwnerReferences = revised.OwnerReferences
	if updated.Labels == nil {
		updated.Labels = make(map[string]string, len(revised.Labels))
	}
	maps.Copy(updated.Labels, revised.Labels)

	if apiequality.Semantic.DeepEqual(current, updated) {
		return nil
	}

	logger.InfoContext(ctx, "updating existing PodDisruptionBudget")
	return splutil.UpdateResource(ctx, client, updated)
}

func sameController(current, desired metav1.Object) bool {
	currentOwner := metav1.GetControllerOf(current)
	desiredOwner := metav1.GetControllerOf(desired)

	if currentOwner == nil || desiredOwner == nil {
		return currentOwner == nil && desiredOwner == nil
	}

	if currentOwner.UID != "" || desiredOwner.UID != "" {
		return currentOwner.UID == desiredOwner.UID
	}

	return currentOwner.APIVersion == desiredOwner.APIVersion &&
		currentOwner.Kind == desiredOwner.Kind &&
		currentOwner.Name == desiredOwner.Name
}
