// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package spark

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// GetSparkAppLabels returns a map of labels to use for Spark instances.
func GetSparkAppLabels(identifier string, typeLabel string) map[string]string {
	labels := map[string]string{
		"app":  "spark",
		"for":  identifier,
		"type": typeLabel,
	}

	return labels
}

// GetSparkMasterPorts returns a map of ports to use for Spark master instances.
func GetSparkMasterPorts() map[string]int {
	return map[string]int{
		"sparkmaster": 7777,
		"sparkwebui":  8009,
	}
}

// GetSparkMasterContainerPorts returns a list of Kubernetes ContainerPort objects for Spark master instances.
func GetSparkMasterContainerPorts() []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range GetSparkMasterPorts() {
		l = append(l, corev1.ContainerPort{
			Name:          key,
			ContainerPort: int32(value),
		})
	}
	return l
}

// GetSparkMasterServicePorts returns a list of Kubernetes ServicePort objects for Spark master instances.
func GetSparkMasterServicePorts() []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range GetSparkMasterPorts() {
		l = append(l, corev1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}

// GetSparkMasterConfiguration returns a list of Kubernetes EnvVar objects for Spark master instances.
func GetSparkMasterConfiguration() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_ROLE",
			Value: "splunk_spark_master",
		},
	}
}

// GetSparkWorkerPorts returns a map of ports to use for Spark worker instances.
func GetSparkWorkerPorts() map[string]int {
	return map[string]int{
		"dfwreceivedata": 17500,
		"workerwebui":    7000,
	}
}

// GetSparkWorkerContainerPorts returns a list of Kubernetes ContainerPort objects for Spark worker instances.
func GetSparkWorkerContainerPorts() []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range GetSparkWorkerPorts() {
		l = append(l, corev1.ContainerPort{
			Name:          key,
			ContainerPort: int32(value),
		})
	}
	return l
}

// GetSparkWorkerServicePorts returns a list of Kubernetes ServicePort objects for Spark worker instances.
func GetSparkWorkerServicePorts() []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range GetSparkWorkerPorts() {
		l = append(l, corev1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}

// GetSparkWorkerConfiguration returns a list of Kubernetes EnvVar objects for Spark worker instances.
func GetSparkWorkerConfiguration(identifier string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_ROLE",
			Value: "splunk_spark_worker",
		}, {
			Name:  "SPARK_MASTER_HOSTNAME",
			Value: GetSparkServiceName(SPARK_MASTER, identifier),
		},
	}
}

// GetSparkRequirements returns the Kubernetes ResourceRequirements to use for Spark instances.
func GetSparkRequirements(cr *v1alpha1.SplunkEnterprise) (corev1.ResourceRequirements, error) {
	cpuRequest, err := resources.ParseResourceQuantity(cr.Spec.Resources.SparkCpuRequest, "0.1")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkCpuRequest", err)
	}

	memoryRequest, err := resources.ParseResourceQuantity(cr.Spec.Resources.SparkMemoryRequest, "512Mi")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkMemoryRequest", err)
	}

	cpuLimit, err := resources.ParseResourceQuantity(cr.Spec.Resources.SparkCpuLimit, "4")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkCpuLimit", err)
	}

	memoryLimit, err := resources.ParseResourceQuantity(cr.Spec.Resources.SparkMemoryLimit, "8Gi")
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
		}}, nil
}

// GetSparkDeployment returns a Kubernetes Deployment object for the Spark master configured for a SplunkEnterprise resource.
func GetSparkDeployment(cr *v1alpha1.SplunkEnterprise, instanceType SparkInstanceType, replicas int, envVariables []corev1.EnvVar, ports []corev1.ContainerPort) (*appsv1.Deployment, error) {

	labels := GetSparkAppLabels(cr.GetIdentifier(), instanceType.ToString())
	replicas32 := int32(replicas)

	requirements, err := GetSparkRequirements(cr)
	if err != nil {
		return nil, err
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSparkDeploymentName(instanceType, cr.GetIdentifier()),
			Namespace: cr.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Replicas: &replicas32,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Affinity:      cr.Spec.Affinity,
					SchedulerName: cr.Spec.SchedulerName,
					Hostname:      GetSparkServiceName(instanceType, cr.GetIdentifier()),
					Containers: []corev1.Container{
						{
							Image:           GetSparkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name:            "spark",
							Ports:           ports,
							Env:             envVariables,
							Resources:       requirements,
						},
					},
				},
			},
		},
	}

	deployment.SetOwnerReferences(append(deployment.GetOwnerReferences(), resources.AsOwner(cr)))

	return deployment, nil
}

// GetSparkStatefulSet returns a Kubernetes StatefulSet object for Spark workers configured for a SplunkEnterprise resource.
func GetSparkStatefulSet(cr *v1alpha1.SplunkEnterprise, instanceType SparkInstanceType, replicas int, envVariables []corev1.EnvVar, containerPorts []corev1.ContainerPort) (*appsv1.StatefulSet, error) {

	labels := GetSparkAppLabels(cr.GetIdentifier(), instanceType.ToString())
	replicas32 := int32(replicas)

	requirements, err := GetSparkRequirements(cr)
	if err != nil {
		return nil, err
	}

	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSparkStatefulsetName(instanceType, cr.GetIdentifier()),
			Namespace: cr.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName:         GetSparkHeadlessServiceName(instanceType, cr.GetIdentifier()),
			Replicas:            &replicas32,
			PodManagementPolicy: "Parallel",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Affinity:      cr.Spec.Affinity,
					SchedulerName: cr.Spec.SchedulerName,
					Containers: []corev1.Container{
						{
							Image:           GetSparkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name:            "spark",
							Ports:           containerPorts,
							Env:             envVariables,
							Resources:       requirements,
						},
					},
				},
			},
		},
	}

	statefulSet.SetOwnerReferences(append(statefulSet.GetOwnerReferences(), resources.AsOwner(cr)))

	return statefulSet, nil
}

// GetSparkService returns a Kubernetes Service object for Spark instances configured for a SplunkEnterprise resource.
func GetSparkService(cr *v1alpha1.SplunkEnterprise, instanceType SparkInstanceType, isHeadless bool, ports []corev1.ServicePort) *corev1.Service {

	serviceName := GetSparkServiceName(instanceType, cr.GetIdentifier())
	if isHeadless {
		serviceName = GetSparkHeadlessServiceName(instanceType, cr.GetIdentifier())
	}

	serviceType := resources.SERVICE
	if isHeadless {
		serviceType = resources.HEADLESS_SERVICE
	}

	serviceTypeLabels := GetSparkAppLabels(cr.GetIdentifier(), serviceType.ToString())
	selectLabels := GetSparkAppLabels(cr.GetIdentifier(), instanceType.ToString())

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cr.Namespace,
			Labels:    serviceTypeLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectLabels,
			Ports:    ports,
		},
	}

	if isHeadless {
		service.Spec.ClusterIP = corev1.ClusterIPNone
	}

	service.SetOwnerReferences(append(service.GetOwnerReferences(), resources.AsOwner(cr)))

	return service
}
