package resources

import (
	"k8s.io/api/core/v1"
	"log"
	"context"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


func CreateResource(client client.Client, obj runtime.Object) error {
	err := client.Create(context.TODO(), obj)
	if err != nil && !errors.IsAlreadyExists(err) {
		log.Printf("Failed to create object : %v", err)
		return err
	}
	return nil
}


func AddOwnerRefToObject(object metav1.Object, reference metav1.OwnerReference) {
	object.SetOwnerReferences(append(object.GetOwnerReferences(), reference))
}


func AsOwner(instance *v1alpha1.SplunkEnterprise) metav1.OwnerReference {
	trueVar := true

	return metav1.OwnerReference{
		APIVersion: instance.TypeMeta.APIVersion,
		Kind:       instance.TypeMeta.Kind,
		Name:       instance.Name,
		UID:        instance.UID,
		Controller: &trueVar,
	}
}


func AddConfigMapVolumeToPodTemplate(podTemplateSpec *v1.PodTemplateSpec, volumeName string, configMapName string, mountLocation string) {
	podTemplateSpec.Spec.Volumes = []v1.Volume{
		{
			Name: volumeName,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: configMapName,
					},
				},
			},
		},
	}
	for idx, _ := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].VolumeMounts = []v1.VolumeMount{
			{
				Name: volumeName,
				MountPath: mountLocation,
			},
		}
	}
}
