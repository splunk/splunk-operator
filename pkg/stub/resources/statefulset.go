package resources


import (
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
	"operator/splunk-operator/pkg/stub/splunk"
)


func CreateSplunkStatefulSet(cr *v1alpha1.SplunkInstance, instanceType splunk.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar) error {

	labels := splunk.GetSplunkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	statefulSet := &v1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: splunk.GetSplunkStatefulsetName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: splunk.GetSplunkHeadlessServiceName(instanceType, identifier),
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

	AddOwnerRefToObject(statefulSet, AsOwner(cr))

	err := CreateResource(statefulSet)
	if err != nil {
		return err
	}

	err = CreateService(cr, instanceType, identifier, true)
	if err != nil {
		return err
	}

	return nil
}