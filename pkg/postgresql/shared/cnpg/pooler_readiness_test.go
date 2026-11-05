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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func readyPoolerWorkload(name, namespace string, side cnpgv1.PoolerType) (*cnpgv1.Pooler, *appsv1.Deployment) {
	pooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(name + "-pooler-uid")},
		Spec:       cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: "green"}, Type: side, Instances: new(int32(2))},
		Status:     cnpgv1.PoolerStatus{Phase: cnpgv1.PoolerPhaseActive, Instances: 2},
	}
	selector := map[string]string{"cnpg.io/poolerName": name}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(name + "-deployment-uid"), Generation: 3,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}},
		Spec: appsv1.DeploymentSpec{Replicas: new(int32(2)), Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pgbouncer", Image: "pgbouncer:test", Ports: []corev1.ContainerPort{{Name: "pgbouncer", ContainerPort: 5432, Protocol: corev1.ProtocolTCP}}}}}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 3, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2},
	}
	return pooler, deployment
}

func TestPoolerDeploymentReadyRequiresAnObservedAvailableOwnedRollout(t *testing.T) {
	tests := []struct {
		name   string
		change func(*cnpgv1.Pooler, *appsv1.Deployment, *corev1.Service)
		want   bool
	}{
		{name: "complete rollout", want: true},
		{name: "CNPG count can lag Deployment readiness", want: true, change: func(p *cnpgv1.Pooler, _ *appsv1.Deployment, _ *corev1.Service) { p.Status.Instances = 0 }},
		{name: "scheduled pods are not ready", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.Status.ReadyReplicas = 0
			d.Status.AvailableReplicas = 0
		}},
		{name: "minimum availability is insufficient", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.Status.AvailableReplicas = 1
			d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}
		}},
		{name: "stale observed generation", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) { d.Generation++ }},
		{name: "old replicas still ready", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) { d.Status.UpdatedReplicas = 0 }},
		{name: "surge replicas still running", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) { d.Status.Replicas = 3 }},
		{name: "terminating replicas remain", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.Status.TerminatingReplicas = new(int32(1))
		}},
		{name: "Deployment scale does not match Pooler", change: func(p *cnpgv1.Pooler, _ *appsv1.Deployment, _ *corev1.Service) { p.Spec.Instances = new(int32(3)) }},
		{name: "zero replicas is not ready", change: func(p *cnpgv1.Pooler, _ *appsv1.Deployment, _ *corev1.Service) { p.Spec.Instances = new(int32(0)) }},
		{name: "pause requested before status catches up", change: func(p *cnpgv1.Pooler, _ *appsv1.Deployment, _ *corev1.Service) {
			p.Spec.PgBouncer = &cnpgv1.PgBouncerSpec{Paused: new(true)}
		}},
		{name: "paused Pooler", change: func(p *cnpgv1.Pooler, _ *appsv1.Deployment, _ *corev1.Service) {
			p.Status.Phase = cnpgv1.PoolerPhasePaused
		}},
		{name: "failed Pooler", change: func(p *cnpgv1.Pooler, _ *appsv1.Deployment, _ *corev1.Service) {
			p.Status.Phase = cnpgv1.PoolerPhaseFailed
		}},
		{name: "paused Deployment", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) { d.Spec.Paused = true }},
		{name: "failed rollout", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse}}
		}},
		{name: "replica failure", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue}}
		}},
		{name: "old Pooler UID", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.OwnerReferences[0].UID = "old-pooler"
		}},
		{name: "wrong controller kind", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) { d.OwnerReferences[0].Kind = "Cluster" }},
		{name: "terminating Deployment", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			now := metav1.Now()
			d.DeletionTimestamp = &now
			d.Finalizers = []string{"test"}
		}},
		{name: "terminating Pooler", change: func(p *cnpgv1.Pooler, _ *appsv1.Deployment, _ *corev1.Service) {
			now := metav1.Now()
			p.DeletionTimestamp = &now
		}},
		{name: "Service belongs to another Pooler", change: func(_ *cnpgv1.Pooler, _ *appsv1.Deployment, s *corev1.Service) {
			s.OwnerReferences[0].UID = "other-pooler"
		}},
		{name: "Service selector misses Deployment pods", change: func(_ *cnpgv1.Pooler, _ *appsv1.Deployment, s *corev1.Service) { s.Spec.Selector["extra"] = "missing" }},
		{name: "Deployment selector misses pod template", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"other": "pods"}}
		}},
		{name: "missing named target port", change: func(_ *cnpgv1.Pooler, d *appsv1.Deployment, _ *corev1.Service) {
			d.Spec.Template.Spec.Containers[0].Ports[0].Name = "metrics"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pooler, deployment := readyPoolerWorkload("green-pooler-rw", "ns", cnpgv1.PoolerTypeRW)
			service := candidateTestObjects(pooler.Name)[0].(*corev1.Service)
			service.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}
			if tt.change != nil {
				tt.change(pooler, deployment, service)
			}
			ready, err := PoolerDeploymentReady(t.Context(), candidateTestReader(t, deployment), pooler, service)
			require.NoError(t, err)
			require.Equal(t, tt.want, ready)
		})
	}
}
