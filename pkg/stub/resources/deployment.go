package resources


import (
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
	"operator/splunk-operator/pkg/stub/splunk"
)


func CreateSplunkDeployment(cr *v1alpha1.SplunkInstance, instanceType splunk.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, DNSConfigSearches []string) error {

	labels := splunk.GetSplunkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	deployment := &v1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: splunk.GetSplunkDeploymentName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Replicas: &replicas32,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: splunk.SPLUNK_IMAGE,
							Name: "splunk",
							Ports: splunk.GetSplunkPorts(),
							Env: envVariables,
						},
					},
					ImagePullSecrets: splunk.GetImagePullSecrets(),
				},
			},
		},
	}

	if DNSConfigSearches != nil {
		deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
		deployment.Spec.Template.Spec.DNSConfig = &corev1.PodDNSConfig{
			Searches: DNSConfigSearches,
		}
	}

	AddOwnerRefToObject(deployment, AsOwner(cr))

	err := CreateResource(deployment)
	if err != nil {
		return err
	}

	return nil
}