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

package kvservice

import (
	"context"
	"fmt"
	"maps"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// reconcileAutoscaler creates, updates, or removes the owned HPA.
func reconcileAutoscaler(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) error {
	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      resources.KVServiceHorizontalPodAutoscalerName(cr),
	}
	current := &autoscalingv2.HorizontalPodAutoscaler{}
	err := c.Get(ctx, namespacedName, current)
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("get KVService HPA %s/%s: %w", namespacedName.Namespace, namespacedName.Name, err)
	}
	if cr.Spec.Autoscaling == nil {
		if k8serrors.IsNotFound(err) || !metav1.IsControlledBy(current, cr) {
			return nil
		}
		if err := c.Delete(ctx, current); err != nil {
			return fmt.Errorf("delete KVService HPA %s/%s: %w", namespacedName.Namespace, namespacedName.Name, err)
		}
		return nil
	}

	desired := resources.NewKVServiceHorizontalPodAutoscaler(cr)
	if k8serrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			return fmt.Errorf("create KVService HPA %s/%s: %w", namespacedName.Namespace, namespacedName.Name, err)
		}
		return nil
	}
	if !metav1.IsControlledBy(current, cr) {
		return fmt.Errorf("HorizontalPodAutoscaler %s/%s already exists and is not controlled by KVService %s", current.Namespace, current.Name, cr.Name)
	}

	if apiequality.Semantic.DeepEqual(current.Spec, desired.Spec) &&
		maps.Equal(current.Labels, desired.Labels) &&
		maps.Equal(current.Annotations, desired.Annotations) &&
		apiequality.Semantic.DeepEqual(current.OwnerReferences, desired.OwnerReferences) {
		return nil
	}

	current.Spec = desired.Spec
	current.Labels = desired.Labels
	current.Annotations = desired.Annotations
	current.OwnerReferences = desired.OwnerReferences
	if err := c.Update(ctx, current); err != nil {
		return fmt.Errorf("update KVService HPA %s/%s: %w", namespacedName.Namespace, namespacedName.Name, err)
	}
	return nil
}
