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
	service.Spec.Ports = append(service.Spec.Ports, splcommon.SortServicePorts([]corev1.ServicePort{getKVServiceServicePort(spec)})...)

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

	ports := splcommon.SortContainerPorts([]corev1.ContainerPort{getKVServiceContainerPort(spec)})
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
						Command:         []string{"kvstore-server"},
						Args:            []string{"--scheme", kvServiceProtocol(spec)},
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
	configureKVServicePortsAndVolumes(&deployment.Spec.Template, spec)
	configureKVServiceSecurity(&deployment.Spec.Template)
	deployment.Spec.Template.Spec.Containers[0].Resources = spec.Resources
	if len(spec.ExtraEnv) > 0 {
		deployment.Spec.Template.Spec.Containers[0].Env = RemoveDuplicateEnvVars(spec.ExtraEnv)
	}
	ConfigureKVServiceHTTPProbes(&deployment.Spec.Template, spec)
	return deployment
}

func kvServiceProtocol(spec *enterpriseApi.KVServiceSpec) string {
	if len(spec.Certs) > 0 {
		return "https"
	}
	return "http"
}

func kvServicePortName(spec *enterpriseApi.KVServiceSpec) string {
	return splutil.GetPortName("kvservice", kvServiceProtocol(spec))
}

func getKVServiceContainerPort(spec *enterpriseApi.KVServiceSpec) corev1.ContainerPort {
	return corev1.ContainerPort{
		Name:          kvServicePortName(spec),
		ContainerPort: splcommon.KVServicePort,
		Protocol:      corev1.ProtocolTCP,
	}
}

func getKVServiceServicePort(spec *enterpriseApi.KVServiceSpec) corev1.ServicePort {
	return corev1.ServicePort{
		Name:       kvServicePortName(spec),
		Port:       splcommon.KVServicePort,
		TargetPort: intstr.FromInt(splcommon.KVServicePort),
		Protocol:   corev1.ProtocolTCP,
	}
}

func kvServiceProbeScheme(spec *enterpriseApi.KVServiceSpec) corev1.URIScheme {
	if len(spec.Certs) > 0 {
		return corev1.URISchemeHTTPS
	}
	return corev1.URISchemeHTTP
}

// ConfigureKVServiceHTTPProbes applies configured TLS scheme and probe timing settings.
func ConfigureKVServiceHTTPProbes(template *corev1.PodTemplateSpec, spec *enterpriseApi.KVServiceSpec) {

	kvserviceLivenessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/service/liveness",
				Port:   intstr.FromInt(splcommon.KVServicePort),
				Scheme: kvServiceProbeScheme(spec),
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
				Path:   "/service/readiness",
				Port:   intstr.FromInt(splcommon.KVServicePort),
				Scheme: kvServiceProbeScheme(spec),
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
			liveness := GetProbeWithConfigUpdates(kvserviceLivenessProbe, spec.LivenessProbe, spec.LivenessInitialDelaySeconds)
			liveness.ProbeHandler = kvserviceLivenessProbe.ProbeHandler
			liveness.SuccessThreshold = 1
			template.Spec.Containers[idx].LivenessProbe = liveness
			readiness := GetProbeWithConfigUpdates(kvserviceReadinessProbe, spec.ReadinessProbe, spec.ReadinessInitialDelaySeconds)
			readiness.ProbeHandler = kvserviceReadinessProbe.ProbeHandler
			readiness.SuccessThreshold = 1
			template.Spec.Containers[idx].ReadinessProbe = readiness
			template.Spec.Containers[idx].StartupProbe = nil
			break
		}
	}

}

// configureKVServiceSecurity applies KVService pod and container security settings.
func configureKVServiceSecurity(podTemplateSpec *corev1.PodTemplateSpec) {
	// update security context
	runAsUser := int64(41812)
	fsGroup := int64(41812)
	runAsNonRoot := true
	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	podTemplateSpec.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser:           &runAsUser,
		FSGroup:             &fsGroup,
		RunAsNonRoot:        &runAsNonRoot,
		FSGroupChangePolicy: &fsGroupChangePolicy,
	}

	privileged := false
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].SecurityContext = &corev1.SecurityContext{
			RunAsUser:                &runAsUser,
			RunAsNonRoot:             &runAsNonRoot,
			AllowPrivilegeEscalation: &[]bool{false}[0],
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
				Add: []corev1.Capability{
					"NET_BIND_SERVICE",
				},
			},
			Privileged: &privileged,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		}
	}
}

// configureKVServicePortsAndVolumes adds user-defined ports and volume mounts.
func configureKVServicePortsAndVolumes(podTemplateSpec *corev1.PodTemplateSpec, spec *enterpriseApi.KVServiceSpec) {
	// Add the ports from the KVService Service template.
	if spec.ServiceTemplate.Spec.Ports != nil {
		for idx := range podTemplateSpec.Spec.Containers {
			for _, p := range spec.ServiceTemplate.Spec.Ports {

				podTemplateSpec.Spec.Containers[idx].Ports = append(podTemplateSpec.Spec.Containers[idx].Ports, corev1.ContainerPort{
					Name:          p.Name,
					ContainerPort: int32(p.TargetPort.IntValue()),
					Protocol:      p.Protocol,
				})
			}
		}
	}

	// Mount user-provided volumes under /mnt/<volume-name>.
	if spec.Volumes != nil {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, spec.Volumes...)
		for idx := range podTemplateSpec.Spec.Containers {
			for v := range spec.Volumes {
				podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(podTemplateSpec.Spec.Containers[idx].VolumeMounts, corev1.VolumeMount{
					Name:      spec.Volumes[v].Name,
					MountPath: "/mnt/" + spec.Volumes[v].Name,
				})
			}
		}
	}

}

// SetKVServiceVolumeDefaults sets default modes for KVService Secret and ConfigMap volumes.
func SetKVServiceVolumeDefaults(spec *enterpriseApi.KVServiceSpec) {
	if spec.Volumes == nil {
		spec.Volumes = []corev1.Volume{}
	}
	for _, v := range spec.Volumes {
		if v.Secret != nil {
			if v.Secret.DefaultMode == nil {
				perm := corev1.SecretVolumeSourceDefaultMode
				v.Secret.DefaultMode = &perm
			}
			continue
		}
		if v.ConfigMap != nil {
			if v.ConfigMap.DefaultMode == nil {
				perm := corev1.ConfigMapVolumeSourceDefaultMode
				v.ConfigMap.DefaultMode = &perm
			}
		}
	}
}
