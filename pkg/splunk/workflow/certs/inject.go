// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certs

import corev1 "k8s.io/api/core/v1"

// InjectCertMounts appends cert volumes, mounts, and rotation annotations into
// the pod template. If config is nil this is a no-op.
// Volumes are added at pod level; VolumeMounts are added to every container.
func InjectCertMounts(pod *corev1.PodTemplateSpec, config *CertMountConfig) {
	if config == nil {
		return
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, config.Volumes...)
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, config.VolumeMounts...)
	}
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	for k, v := range config.Annotations {
		pod.Annotations[k] = v
	}
}
