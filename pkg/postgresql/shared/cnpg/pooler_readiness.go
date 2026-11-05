/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cnpg

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PoolerDeploymentReady verifies the Pooler's owned Deployment and Service,
// including a fully observed, available rollout of the desired replica count.
// Endpoint publication supplies an uncached reader for its safety checks.
// This proves workload readiness, not Service endpoint convergence or SQL access.
func PoolerDeploymentReady(ctx context.Context, reader client.Reader, pooler *cnpgv1.Pooler, service *corev1.Service) (bool, error) {
	if reader == nil || pooler == nil || service == nil {
		return false, fmt.Errorf("Pooler Deployment readiness requires a reader, Pooler and Service")
	}
	desired := int32(1)
	if pooler.Spec.Instances != nil {
		desired = *pooler.Spec.Instances
	}
	paused := pooler.Spec.PgBouncer != nil && pooler.Spec.PgBouncer.Paused != nil && *pooler.Spec.PgBouncer.Paused
	if paused || desired < 1 || pooler.UID == "" || pooler.DeletionTimestamp != nil || pooler.Status.Phase != cnpgv1.PoolerPhaseActive {
		return false, nil
	}
	if service.Name != pooler.Name || service.Namespace != pooler.Namespace || service.DeletionTimestamp != nil || !ownedByPooler(service, pooler) {
		return false, nil
	}
	deployment := &appsv1.Deployment{}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(pooler), deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading Pooler Deployment %s: %w", pooler.Name, err)
	}
	if deployment.UID == "" || deployment.DeletionTimestamp != nil || !ownedByPooler(deployment, pooler) || deployment.Spec.Paused {
		return false, nil
	}
	if !poolerServiceSelectsDeployment(service, deployment) {
		return false, nil
	}
	return poolerRolloutReady(deployment, desired), nil
}

func ownedByPooler(object metav1.Object, pooler *cnpgv1.Pooler) bool {
	owner := metav1.GetControllerOf(object)
	return owner != nil && owner.APIVersion == cnpgv1.SchemeGroupVersion.String() && owner.Kind == "Pooler" && owner.Name == pooler.Name && owner.UID == pooler.UID
}

func poolerServiceSelectsDeployment(service *corev1.Service, deployment *appsv1.Deployment) bool {
	if len(service.Spec.Selector) == 0 || deployment.Spec.Selector == nil {
		return false
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || selector.Empty() || !selector.Matches(labels.Set(deployment.Spec.Template.Labels)) {
		return false
	}
	if !labels.SelectorFromSet(service.Spec.Selector).Matches(labels.Set(deployment.Spec.Template.Labels)) {
		return false
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		for _, port := range container.Ports {
			if port.Name == "pgbouncer" && port.ContainerPort > 0 && (port.Protocol == "" || port.Protocol == corev1.ProtocolTCP) {
				return true
			}
		}
	}
	return false
}

func poolerRolloutReady(deployment *appsv1.Deployment, desired int32) bool {
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	status := deployment.Status
	if replicas != desired || deployment.Generation <= 0 || status.ObservedGeneration < deployment.Generation ||
		status.Replicas != desired || status.UpdatedReplicas != desired || status.ReadyReplicas != desired ||
		status.AvailableReplicas != desired || status.UnavailableReplicas != 0 ||
		(status.TerminatingReplicas != nil && *status.TerminatingReplicas != 0) {
		return false
	}
	for _, condition := range status.Conditions {
		if (condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue) ||
			(condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse) {
			return false
		}
	}
	return true
}
