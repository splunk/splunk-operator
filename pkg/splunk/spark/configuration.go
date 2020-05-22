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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// getSparkLabels returns a map of labels to use for Spark components.
func getSparkLabels(identifier string, instanceType InstanceType) map[string]string {
	return resources.GetLabels("spark", instanceType.ToString(), identifier)
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
			Name:       key,
			Port:       int32(value),
			TargetPort: intstr.FromInt(value),
			Protocol:   corev1.ProtocolTCP,
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
			Name:       key,
			Port:       int32(value),
			TargetPort: intstr.FromInt(value),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	return l
}

// ValidateSparkSpec checks validity and makes default updates to a SparkSpec, and returns error if something is wrong.
func ValidateSparkSpec(spec *enterprisev1.SparkSpec) error {
	spec.CommonSpec.Image = GetSparkImage(spec.CommonSpec.Image)
	if spec.Replicas == 0 {
		spec.Replicas = 1
	}
	defaultResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0.1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		},
	}
	return resources.ValidateCommonSpec(&spec.CommonSpec, defaultResources)
}

// GetSparkDeployment returns a Kubernetes Deployment object for the Spark master configured for a Spark resource.
func GetSparkDeployment(cr *enterprisev1.Spark, instanceType InstanceType) (*appsv1.Deployment, error) {
	// prepare type specific variables (note that port order is important for tests)
	var ports []corev1.ContainerPort
	var envVariables []corev1.EnvVar
	var replicas int32
	switch instanceType {
	case SparkMaster:
		ports = resources.SortContainerPorts(getSparkMasterContainerPorts())
		envVariables = []corev1.EnvVar{
			{
				Name:  "SPLUNK_ROLE",
				Value: "splunk_spark_master",
			},
		}
		replicas = 1
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
		replicas = int32(cr.Spec.Replicas)
	}

	// prepare labels, annotations and affinity
	annotations := resources.GetIstioAnnotations(ports)
	affinity := resources.AppendPodAntiAffinity(&cr.Spec.Affinity, cr.GetIdentifier(), instanceType.ToString())
	selectLabels := getSparkLabels(cr.GetIdentifier(), instanceType)
	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	// create deployment configuration
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSparkDeploymentName(instanceType, cr.GetIdentifier()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selectLabels,
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Affinity:      affinity,
					Tolerations:   cr.Spec.Tolerations,
					SchedulerName: cr.Spec.SchedulerName,
					Hostname:      GetSparkServiceName(instanceType, cr.GetIdentifier(), false),
					Containers: []corev1.Container{
						{
							Image:           cr.Spec.Image,
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

	// append labels and annotations from parent
	resources.AppendParentMeta(deployment.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())

	// make Spark object the owner
	deployment.SetOwnerReferences(append(deployment.GetOwnerReferences(), resources.AsOwner(cr)))

	// update with common spark pod config
	err := updateSparkPodTemplateWithConfig(&deployment.Spec.Template, cr, instanceType)
	if err != nil {
		return nil, err
	}

	return deployment, nil
}

// GetSparkService returns a Kubernetes Service object for Spark instances configured for a Spark resource.
func GetSparkService(cr *enterprisev1.Spark, instanceType InstanceType, isHeadless bool) *corev1.Service {

	// use template if not headless
	var service *corev1.Service
	if isHeadless {
		service = &corev1.Service{}
		service.Spec.ClusterIP = corev1.ClusterIPNone
		service.Spec.Type = corev1.ServiceTypeClusterIP
	} else {
		service = cr.Spec.ServiceTemplate.DeepCopy()
	}
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}
	service.ObjectMeta.Name = GetSparkServiceName(instanceType, cr.GetIdentifier(), isHeadless)
	service.ObjectMeta.Namespace = cr.GetNamespace()
	service.Spec.Selector = getSparkLabels(cr.GetIdentifier(), instanceType)

	// prepare ports (note that port order is important for tests)
	switch instanceType {
	case SparkMaster:
		service.Spec.Ports = resources.SortServicePorts(getSparkMasterServicePorts())
	case SparkWorker:
		service.Spec.Ports = resources.SortServicePorts(getSparkWorkerServicePorts())
	}

	// ensure labels and annotations are not nil
	if service.ObjectMeta.Labels == nil {
		service.ObjectMeta.Labels = make(map[string]string)
	}
	if service.ObjectMeta.Annotations == nil {
		service.ObjectMeta.Annotations = make(map[string]string)
	}

	// append same labels as selector
	for k, v := range service.Spec.Selector {
		service.ObjectMeta.Labels[k] = v
	}

	// append labels and annotations from parent
	resources.AppendParentMeta(service.GetObjectMeta(), cr.GetObjectMeta())

	service.SetOwnerReferences(append(service.GetOwnerReferences(), resources.AsOwner(cr)))

	return service
}

// updateSparkPodTemplateWithConfig modifies the podTemplateSpec object based on configuration of the Spark resource.
func updateSparkPodTemplateWithConfig(podTemplateSpec *corev1.PodTemplateSpec, cr *enterprisev1.Spark, instanceType InstanceType) error {

	// update security context
	runAsUser := int64(41812)
	fsGroup := int64(41812)
	podTemplateSpec.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser: &runAsUser,
		FSGroup:   &fsGroup,
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
		podTemplateSpec.Spec.Containers[idx].Resources = cr.Spec.Resources
		podTemplateSpec.Spec.Containers[idx].LivenessProbe = livenessProbe
		podTemplateSpec.Spec.Containers[idx].ReadinessProbe = readinessProbe
	}

	return nil
}
