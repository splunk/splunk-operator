package resources

import (
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"log"
	"context"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	corev1 "k8s.io/api/core/v1"
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
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, v1.Volume{
		Name: volumeName,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	})
	for idx, _ := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].VolumeMounts = []v1.VolumeMount{
			{
				Name: volumeName,
				MountPath: mountLocation,
			},
		}
	}
}


func AddLicenseVolumeToPodTemplate(podTemplateSpec *v1.PodTemplateSpec, volumeName string, source *v1.VolumeSource, mountLocation string) {
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, v1.Volume{
		Name: volumeName,
		VolumeSource: *source,
	})
	for idx, _ := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].VolumeMounts = []v1.VolumeMount{
			{
				Name: volumeName,
				MountPath: mountLocation,
			},
		}
	}
}


func ParseResourceQuantity(str string, useIfEmpty string) (resource.Quantity, error) {
	var result resource.Quantity

	if (str == "") {
		if (useIfEmpty != "") {
			result = resource.MustParse(useIfEmpty)
		}
	} else {
		result, err := resource.ParseQuantity(str)
		if err != nil {
			return result, fmt.Errorf("Invalid resource quantity \"%s\": %s", str, err)
		}
	}

	return result, nil
}



func GetSplunkRequirements(cr *v1alpha1.SplunkEnterprise) (corev1.ResourceRequirements, error) {
	cpuRequest, err := ParseResourceQuantity(cr.Spec.Config.SplunkCpuRequest, "0.1")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SplunkCpuRequest", err)
	}

	memoryRequest, err := ParseResourceQuantity(cr.Spec.Config.SplunkMemoryRequest, "1Gi")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SplunkMemoryRequest", err)
	}

	cpuLimit, err := ParseResourceQuantity(cr.Spec.Config.SplunkCpuLimit, "4")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SplunkCpuLimit", err)
	}

	memoryLimit, err := ParseResourceQuantity(cr.Spec.Config.SplunkMemoryLimit, "8Gi")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SplunkMemoryLimit", err)
	}

	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    cpuRequest,
			corev1.ResourceMemory: memoryRequest,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    cpuLimit,
			corev1.ResourceMemory: memoryLimit,
		} }, nil
}


func GetSparkRequirements(cr *v1alpha1.SplunkEnterprise) (corev1.ResourceRequirements, error) {
	cpuRequest, err := ParseResourceQuantity(cr.Spec.Config.SparkCpuRequest, "0.1")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkCpuRequest", err)
	}

	memoryRequest, err := ParseResourceQuantity(cr.Spec.Config.SparkMemoryRequest, "1Gi")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkMemoryRequest", err)
	}

	cpuLimit, err := ParseResourceQuantity(cr.Spec.Config.SparkCpuLimit, "4")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkCpuLimit", err)
	}

	memoryLimit, err := ParseResourceQuantity(cr.Spec.Config.SparkMemoryLimit, "8Gi")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkMemoryLimit", err)
	}

	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    cpuRequest,
			corev1.ResourceMemory: memoryRequest,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    cpuLimit,
			corev1.ResourceMemory: memoryLimit,
		} }, nil
}
