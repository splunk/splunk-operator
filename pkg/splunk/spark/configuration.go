// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package spark

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// getSparkAppLabels returns a map of labels to use for Spark components.
func getSparkAppLabels(identifier string, typeLabel string, isSelector bool) map[string]string {
	labels := resources.GetLabels(identifier, typeLabel, isSelector)
	labels["app"] = "spark"
	return labels
}

// getSparkMasterPorts returns a map of ports to use for Spark master instances.
func getSparkMasterPorts() map[string]int {
	return map[string]int{
		"sparkmaster": 7777,
		"sparkwebui":  8009,
	}
}

// getSparkMasterContainerPorts returns a list of Kubernetes ContainerPort objects for Spark master instances.
func getSparkMasterContainerPorts() []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range getSparkMasterPorts() {
		l = append(l, corev1.ContainerPort{
			Name:          key,
			ContainerPort: int32(value),
			Protocol:      "TCP",
		})
	}
	return l
}

// getSparkMasterServicePorts returns a list of Kubernetes ServicePort objects for Spark master instances.
func getSparkMasterServicePorts() []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range getSparkMasterPorts() {
		l = append(l, corev1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}

// getSparkWorkerPorts returns a map of ports to use for Spark worker instances.
func getSparkWorkerPorts() map[string]int {
	return map[string]int{
		"dfwreceivedata": 17500,
		"workerwebui":    7000,
	}
}

// getSparkWorkerContainerPorts returns a list of Kubernetes ContainerPort objects for Spark worker instances.
func getSparkWorkerContainerPorts() []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range getSparkWorkerPorts() {
		l = append(l, corev1.ContainerPort{
			Name:          key,
			ContainerPort: int32(value),
			Protocol:      "TCP",
		})
	}
	return l
}

// getSparkWorkerServicePorts returns a list of Kubernetes ServicePort objects for Spark worker instances.
func getSparkWorkerServicePorts() []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range getSparkWorkerPorts() {
		l = append(l, corev1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}

// getSparkRequirements returns the Kubernetes ResourceRequirements to use for Spark instances.
func getSparkRequirements(cr *v1alpha2.SplunkEnterprise) (corev1.ResourceRequirements, error) {
	cpuRequest, err := resources.ParseResourceQuantity(cr.Spec.Resources.SparkCPURequest, "0.1")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkCPURequest", err)
	}

	memoryRequest, err := resources.ParseResourceQuantity(cr.Spec.Resources.SparkMemoryRequest, "512Mi")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkMemoryRequest", err)
	}

	cpuLimit, err := resources.ParseResourceQuantity(cr.Spec.Resources.SparkCPULimit, "4")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SparkCPULimit", err)
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
func GetSparkDeployment(cr *v1alpha2.SplunkEnterprise, instanceType InstanceType, replicas int) (*appsv1.Deployment, error) {
	// prepare type specific variables (note that port order is important for tests)
	var ports []corev1.ContainerPort
	var envVariables []corev1.EnvVar
	switch instanceType {
	case SparkMaster:
		ports = resources.SortContainerPorts(getSparkMasterContainerPorts())
		envVariables = []corev1.EnvVar{
			{
				Name:  "SPLUNK_ROLE",
				Value: "splunk_spark_master",
			},
		}
	case SparkWorker:
		ports = resources.SortContainerPorts(getSparkWorkerContainerPorts())
		envVariables = []corev1.EnvVar{
			{
				Name:  "SPLUNK_ROLE",
				Value: "splunk_spark_worker",
			}, {
				Name:  "SPARK_MASTER_HOSTNAME",
				Value: GetSparkServiceName(SparkMaster, cr.GetIdentifier(), false),
			}, {
				Name:  "SPARK_WORKER_PORT", // this is set in new versions of splunk/spark container, but defined here for backwards-compatability
				Value: "7777",
			},
		}
	}

	// prepare other values
	replicas32 := int32(replicas)
	annotations := resources.GetIstioAnnotations(ports)
	affinity := resources.AppendPodAntiAffinity(cr.Spec.Affinity, cr.GetIdentifier(), instanceType.ToString())

	// create deployment configuration
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
				MatchLabels: getSparkAppLabels(cr.GetIdentifier(), instanceType.ToString(), true),
			},
			Replicas: &replicas32,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      getSparkAppLabels(cr.GetIdentifier(), instanceType.ToString(), false),
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Affinity:      affinity,
					SchedulerName: cr.Spec.SchedulerName,
					Hostname:      GetSparkServiceName(instanceType, cr.GetIdentifier(), false),
					Containers: []corev1.Container{
						{
							Image:           GetSparkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name:            "spark",
							Ports:           ports,
							Env:             envVariables,
						},
					},
				},
			},
		},
	}

	// make SplunkEnterprise object the owner
	deployment.SetOwnerReferences(append(deployment.GetOwnerReferences(), resources.AsOwner(cr)))

	// update with common spark pod config
	err := updateSparkPodTemplateWithConfig(&deployment.Spec.Template, cr, instanceType)
	if err != nil {
		return nil, err
	}

	return deployment, nil
}

// GetSparkService returns a Kubernetes Service object for Spark instances configured for a SplunkEnterprise resource.
func GetSparkService(cr *v1alpha2.SplunkEnterprise, instanceType InstanceType, isHeadless bool) *corev1.Service {
	// prepare ports (note that port order is important for tests)
	var ports []corev1.ServicePort
	switch instanceType {
	case SparkMaster:
		ports = resources.SortServicePorts(getSparkMasterServicePorts())
	case SparkWorker:
		ports = resources.SortServicePorts(getSparkWorkerServicePorts())
	}

	// prepare other values
	serviceName := GetSparkServiceName(instanceType, cr.GetIdentifier(), isHeadless)
	serviceTypeLabels := getSparkAppLabels(cr.GetIdentifier(), fmt.Sprintf("%s-%s", instanceType, "service"), false)
	selectLabels := getSparkAppLabels(cr.GetIdentifier(), instanceType.ToString(), true)

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

// updateSparkPodTemplateWithConfig modifies the podTemplateSpec object based on configuration of the SplunkEnterprise resource.
func updateSparkPodTemplateWithConfig(podTemplateSpec *corev1.PodTemplateSpec, cr *v1alpha2.SplunkEnterprise, instanceType InstanceType) error {

	// update security context
	runAsUser := int64(41812)
	fsGroup := int64(41812)
	podTemplateSpec.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser: &runAsUser,
		FSGroup:   &fsGroup,
	}

	// prepare resource requirements
	requirements, err := getSparkRequirements(cr)
	if err != nil {
		return err
	}

	// master listens for HTTP requests on a different interface from worker
	var httpPort intstr.IntOrString
	if instanceType == SparkMaster {
		httpPort = intstr.FromInt(8009)
	} else {
		httpPort = intstr.FromInt(7000)
	}

	// probe to check if pod is alive
	livenessProbe := &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: httpPort,
				Path: "/",
			},
		},
		InitialDelaySeconds: 30,
		TimeoutSeconds:      10,
		PeriodSeconds:       10,
	}

	// probe to check if pod is ready
	readinessProbe := &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: httpPort,
				Path: "/",
			},
		},
		InitialDelaySeconds: 5,
		TimeoutSeconds:      10,
		PeriodSeconds:       10,
	}

	// update each container in pod
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].Resources = requirements
		podTemplateSpec.Spec.Containers[idx].LivenessProbe = livenessProbe
		podTemplateSpec.Spec.Containers[idx].ReadinessProbe = readinessProbe
	}

	return nil
}
