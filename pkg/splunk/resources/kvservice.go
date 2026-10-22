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

package resources

import (
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// GetKVServiceService builds the Service without API calls.
func GetKVServiceService(cr splcommon.MetaObject, spec *enterpriseApi.KVServiceSpec) *corev1.Service {

	instanceType := splcommon.SplunkKVService
	var service *corev1.Service

	service = spec.ServiceTemplate.DeepCopy()
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	service.ObjectMeta.Name = splcommon.GetSplunkServiceName(instanceType, cr.GetName(), false)
	service.ObjectMeta.Namespace = cr.GetNamespace()

	instanceIdentifier := cr.GetName()
	var partOfIdentifier string

	service.Spec.Selector = GetSplunkLabels(instanceIdentifier, instanceType, partOfIdentifier)
	service.Spec.Ports = append(service.Spec.Ports, splcommon.SortServicePorts(GetSplunkServicePorts(instanceType))...)

	// ensure labels and annotations are not nil
	if service.ObjectMeta.Labels == nil {
		service.ObjectMeta.Labels = make(map[string]string)
	}
	if service.ObjectMeta.Annotations == nil {
		service.ObjectMeta.Annotations = make(map[string]string)
	}
	// append same labels as selector
	for k, v := range service.Spec.Selector {
		service.ObjectMeta.Labels[k] = v
	}
	// append labels and annotations from parent
	splcommon.AppendParentMeta(service.ObjectMeta.GetObjectMeta(), cr.GetObjectMeta())
	service.SetOwnerReferences(append(service.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	return service
}

// NewKVServiceDeployment builds the desired Deployment without API calls.
func NewKVServiceDeployment(cr *enterpriseApi.KVService) *appsv1.Deployment {
	spec := cr.Spec.DeepCopy()
	instanceType := splcommon.SplunkKVService
	replicas := spec.Replicas

	ports := splcommon.SortContainerPorts(GetSplunkContainerPorts(instanceType))
	annotations := splcommon.GetIstioAnnotations(ports)
	selectLabels := GetSplunkLabels(cr.GetName(), instanceType, "")
	affinity := splcommon.AppendPodAntiAffinity(&spec.Affinity, cr.GetName(), instanceType.ToString())

	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	deployment := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: splutil.GetSplunkDeploymentName(instanceType, cr.Name), Namespace: cr.Namespace, Labels: labels},
	}

	deployment.Spec = appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: selectLabels,
		},
		Replicas: &replicas,
		Strategy: appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				Affinity:                  affinity,
				Tolerations:               spec.Tolerations,
				TopologySpreadConstraints: spec.TopologySpreadConstraints,
				SchedulerName:             spec.SchedulerName,
				ImagePullSecrets:          spec.ImagePullSecrets,
				Containers: []corev1.Container{
					{
						Image:           spec.Image,
						ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
						Name:            "kvservice",
						Ports:           ports,
					},
				},
			},
		},
	}

	deployment.Spec.Template.Spec.ServiceAccountName = spec.ServiceAccount
	splcommon.AppendParentMeta(deployment.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())
	for k, v := range spec.PodAnnotations {
		deployment.Spec.Template.Annotations[k] = v
	}
	deployment.SetOwnerReferences([]metav1.OwnerReference{splcommon.AsOwner(cr, true)})
	ConfigureKVServiceHTTPProbes(&deployment.Spec.Template)
	return deployment
}

// ConfigureKVServiceHTTPProbes applies fixed HTTP endpoints and timings for KVService.
func ConfigureKVServiceHTTPProbes(template *corev1.PodTemplateSpec) {

	kvserviceLivenessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/service/liveness",
				Port: intstr.FromInt(splcommon.KVServicePort),
			},
		},
		InitialDelaySeconds: 30,
		TimeoutSeconds:      1,
		PeriodSeconds:       5,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	kvserviceReadinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/service/readiness",
				Port: intstr.FromInt(splcommon.KVServicePort),
			},
		},
		InitialDelaySeconds: 30,
		TimeoutSeconds:      1,
		PeriodSeconds:       5,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	for idx := range template.Spec.Containers {
		if template.Spec.Containers[idx].Name == "kvservice" {
			template.Spec.Containers[idx].LivenessProbe = kvserviceLivenessProbe
			template.Spec.Containers[idx].ReadinessProbe = kvserviceReadinessProbe
			template.Spec.Containers[idx].StartupProbe = nil
			break
		}
	}

}
