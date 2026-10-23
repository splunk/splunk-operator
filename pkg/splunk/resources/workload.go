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
	corev1 "k8s.io/api/core/v1"
)

// ConfigureWorkloadSecurity applies the shared pod and container security settings.
func ConfigureWorkloadSecurity(podTemplateSpec *corev1.PodTemplateSpec) {
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

// ConfigureWorkloadPortsAndVolumes adds user-defined ports and volume mounts.
func ConfigureWorkloadPortsAndVolumes(podTemplateSpec *corev1.PodTemplateSpec, spec *enterpriseApi.CommonWorkloadSpec) {
	// Add custom ports to splunk containers
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

	// Add custom volumes to splunk containers other than MC(where CR spec volumes are not needed)
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
