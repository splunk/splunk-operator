package  tlsprov
import (
	corev1 "k8s.io/api/core/v1"
)

const (
	tlsVolumeName = "splunk-tls"
	tlsMountPath  = "/mnt/certs"
)

func EnsureTLSMount(pod *corev1.PodTemplateSpec, secretName string) {
	if pod == nil || secretName == "" {
		return
	}
	// volume once
	found := false
	for _, v := range pod.Spec.Volumes {
		if v.Name == tlsVolumeName {
			found = true
			break
		}
	}
	if !found {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: tlsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: secretName},
			},
		})
	}
	// mount in all containers
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		has := false
		for _, m := range c.VolumeMounts {
			if m.Name == tlsVolumeName {
				has = true
				break
			}
		}
		if !has {
			c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
				Name:      tlsVolumeName,
				MountPath: tlsMountPath,
				ReadOnly:  true,
			})
		}
	}
}
