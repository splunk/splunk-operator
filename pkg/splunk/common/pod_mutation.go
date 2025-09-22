package common

import corev1 "k8s.io/api/core/v1"

func EnsureVolume(pod *corev1.PodTemplateSpec, vol corev1.Volume) {
	for _, v := range pod.Spec.Volumes {
		if v.Name == vol.Name {
			return
		}
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, vol)
}

func EnsureMountAllContainers(pod *corev1.PodTemplateSpec, vm corev1.VolumeMount) {
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		found := false
		for _, m := range c.VolumeMounts {
			if m.Name == vm.Name {
				found = true
				break
			}
		}
		if !found {
			c.VolumeMounts = append(c.VolumeMounts, vm)
		}
	}
}
