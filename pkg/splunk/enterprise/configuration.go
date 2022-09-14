// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package enterprise

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getSplunkLabels returns a map of labels to use for Splunk Enterprise components.
func getSplunkLabels(instanceIdentifier string, instanceType InstanceType, partOfIdentifier string) map[string]string {
	// For multisite / multipart IndexerCluster, the name of the part containing the cluster-manager is used
	// to set the label app.kubernetes.io/part-of on all the parts so that its indexer service can select
	// the indexers from all the parts. Otherwise partOfIdentifier is equal to instanceIdentifier.
	if instanceType != SplunkIndexer || len(partOfIdentifier) == 0 {
		partOfIdentifier = instanceIdentifier
	}

	labels, _ := splcommon.GetLabels(instanceType.ToKind(), instanceType.ToString(), instanceIdentifier, partOfIdentifier, make([]string, 0))
	return labels
}

// getSplunkVolumeClaims returns a standard collection of Kubernetes volume claims.
func getSplunkVolumeClaims(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, labels map[string]string, volumeType string) (corev1.PersistentVolumeClaim, error) {
	var storageCapacity resource.Quantity
	var err error

	storageClassName := ""

	// Depending on the volume type, determine storage capacity and storage class name(if configured)
	if volumeType == splcommon.EtcVolumeStorage {
		storageCapacity, err = splcommon.ParseResourceQuantity(spec.EtcVolumeStorageConfig.StorageCapacity, splcommon.DefaultEtcVolumeStorageCapacity)
		if err != nil {
			return corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "etcStorage", err)
		}
		if spec.EtcVolumeStorageConfig.StorageClassName != "" {
			storageClassName = spec.EtcVolumeStorageConfig.StorageClassName
		}
	} else if volumeType == splcommon.VarVolumeStorage {
		storageCapacity, err = splcommon.ParseResourceQuantity(spec.VarVolumeStorageConfig.StorageCapacity, splcommon.DefaultVarVolumeStorageCapacity)
		if err != nil {
			return corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "varStorage", err)
		}
		if spec.VarVolumeStorageConfig.StorageClassName != "" {
			storageClassName = spec.VarVolumeStorageConfig.StorageClassName
		}
	}

	// Create a persistent volume claim
	volumeClaim := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(splcommon.PvcNamePrefix, volumeType),
			Namespace: cr.GetNamespace(),
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageCapacity,
				},
			},
		},
	}

	// Assign storage class name if specified
	if storageClassName != "" {
		volumeClaim.Spec.StorageClassName = &storageClassName
	}
	return volumeClaim, nil
}

// getSplunkService returns a Kubernetes Service object for Splunk instances configured for a Splunk Enterprise resource.
func getSplunkService(ctx context.Context, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType InstanceType, isHeadless bool) *corev1.Service {

	// use template if not headless
	var service *corev1.Service
	if isHeadless {
		service = &corev1.Service{}

		// Initialize to defaults
		service.Spec.ClusterIP = corev1.ClusterIPNone
		service.Spec.Type = corev1.ServiceTypeClusterIP
	} else {
		service = spec.ServiceTemplate.DeepCopy()
	}
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	service.ObjectMeta.Name = GetSplunkServiceName(instanceType, cr.GetName(), isHeadless)
	service.ObjectMeta.Namespace = cr.GetNamespace()
	instanceIdentifier := cr.GetName()
	var partOfIdentifier string
	if instanceType == SplunkIndexer {
		if len(spec.ClusterManagerRef.Name) == 0 && len(spec.ClusterMasterRef.Name) == 0 {
			// Do not specify the instance label in the selector of IndexerCluster services, so that the services of the main part
			// of multisite / multipart IndexerCluster can be used to resolve (headless) or load balance traffic to the indexers of all parts
			partOfIdentifier = instanceIdentifier
			instanceIdentifier = ""
		} else if len(spec.ClusterManagerRef.Name) > 0 {
			// And for child parts of multisite / multipart IndexerCluster, use the name of the part containing the cluster-manager
			// in the app.kubernetes.io/part-of label
			partOfIdentifier = spec.ClusterManagerRef.Name
		} else if len(spec.ClusterMasterRef.Name) > 0 {
			// And for child parts of multisite / multipart IndexerCluster, use the name of the part containing the cluster-manager
			// in the app.kubernetes.io/part-of label
			partOfIdentifier = spec.ClusterMasterRef.Name
		}
	}
	service.Spec.Selector = getSplunkLabels(instanceIdentifier, instanceType, partOfIdentifier)
	service.Spec.Ports = append(service.Spec.Ports, splcommon.SortServicePorts(getSplunkServicePorts(instanceType))...) // note that port order is important for tests

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
	splcommon.AppendParentMeta(service.ObjectMeta.GetObjectMeta(), cr.GetObjectMeta())

	if instanceType == SplunkDeployer || (instanceType == SplunkSearchHead && isHeadless) {
		// required for SHC bootstrap process; use services with heads when readiness is desired
		service.Spec.PublishNotReadyAddresses = true
	}

	service.SetOwnerReferences(append(service.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	return service
}

// setVolumeDefaults set properties in Volumes to default values
func setVolumeDefaults(spec *enterpriseApi.CommonSplunkSpec) {

	// work-around openapi validation error by ensuring it is not nil
	if spec.Volumes == nil {
		spec.Volumes = []corev1.Volume{}
	}

	for _, v := range spec.Volumes {
		if v.Secret != nil {
			if v.Secret.DefaultMode == nil {
				perm := int32(corev1.SecretVolumeSourceDefaultMode)
				v.Secret.DefaultMode = &perm
			}
			continue
		}

		if v.ConfigMap != nil {
			if v.ConfigMap.DefaultMode == nil {
				perm := int32(corev1.ConfigMapVolumeSourceDefaultMode)
				v.ConfigMap.DefaultMode = &perm
			}
			continue
		}
	}
}

// ValidateImagePullPolicy checks validity of the ImagePullPolicy spec parameter, and returns error if it is invalid.
func ValidateImagePullPolicy(imagePullPolicy *string) error {
	// ImagePullPolicy
	if *imagePullPolicy == "" {
		*imagePullPolicy = os.Getenv("IMAGE_PULL_POLICY")
	}
	switch *imagePullPolicy {
	case "":
		*imagePullPolicy = "IfNotPresent"
		break
	case "Always":
		break
	case "IfNotPresent":
		break
	default:
		return fmt.Errorf("ImagePullPolicy must be one of \"Always\" or \"IfNotPresent\"; value=\"%s\"", *imagePullPolicy)
	}
	return nil
}

// ValidateResources checks resource requests and limits and sets defaults if not provided
func ValidateResources(resources *corev1.ResourceRequirements, defaults corev1.ResourceRequirements) {
	// check for nil maps
	if resources.Requests == nil {
		resources.Requests = make(corev1.ResourceList)
	}
	if resources.Limits == nil {
		resources.Limits = make(corev1.ResourceList)
	}

	// if not given, use default cpu requests
	_, ok := resources.Requests[corev1.ResourceCPU]
	if !ok {
		resources.Requests[corev1.ResourceCPU] = defaults.Requests[corev1.ResourceCPU]
	}

	// if not given, use default memory requests
	_, ok = resources.Requests[corev1.ResourceMemory]
	if !ok {
		resources.Requests[corev1.ResourceMemory] = defaults.Requests[corev1.ResourceMemory]
	}

	// if not given, use default cpu limits
	_, ok = resources.Limits[corev1.ResourceCPU]
	if !ok {
		resources.Limits[corev1.ResourceCPU] = defaults.Limits[corev1.ResourceCPU]
	}

	// if not given, use default memory limits
	_, ok = resources.Limits[corev1.ResourceMemory]
	if !ok {
		resources.Limits[corev1.ResourceMemory] = defaults.Limits[corev1.ResourceMemory]
	}
}

// ValidateSpec checks validity and makes default updates to a Spec, and returns error if something is wrong.
func ValidateSpec(spec *enterpriseApi.Spec, defaultResources corev1.ResourceRequirements) error {
	// make sure SchedulerName is not empty
	if spec.SchedulerName == "" {
		spec.SchedulerName = "default-scheduler"
	}

	// set default values for service template
	setServiceTemplateDefaults(spec)

	// if not provided, set default resource requests and limits
	ValidateResources(&spec.Resources, defaultResources)

	return ValidateImagePullPolicy(&spec.ImagePullPolicy)
}

// setServiceTemplateDefaults sets default values for service templates
func setServiceTemplateDefaults(spec *enterpriseApi.Spec) {
	if spec.ServiceTemplate.Spec.Ports != nil {
		for idx := range spec.ServiceTemplate.Spec.Ports {
			var p *corev1.ServicePort = &spec.ServiceTemplate.Spec.Ports[idx]
			if p.Protocol == "" {
				p.Protocol = corev1.ProtocolTCP
			}

			if p.TargetPort.IntValue() == 0 {
				p.TargetPort.IntVal = p.Port
			}
		}
	}

	if spec.ServiceTemplate.Spec.Type == "" {
		spec.ServiceTemplate.Spec.Type = corev1.ServiceTypeClusterIP
	}
}

// validateCommonSplunkSpec checks validity and makes default updates to a CommonSplunkSpec, and returns error if something is wrong.
func validateCommonSplunkSpec(ctx context.Context, c splcommon.ControllerClient, spec *enterpriseApi.CommonSplunkSpec, cr splcommon.MetaObject) error {
	// if not specified via spec or env, image defaults to splunk/splunk
	spec.Image = GetSplunkImage(spec.Image)

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

	if spec.LivenessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Liveness probe intial delay", spec.LivenessInitialDelaySeconds)
	}

	if spec.ReadinessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Readiness probe intial delay", spec.ReadinessInitialDelaySeconds)
	}

	// if not provided, set default values for imagePullSecrets
	err := ValidateImagePullSecrets(ctx, c, cr, spec)
	if err != nil {
		return err
	}

	setVolumeDefaults(spec)

	return ValidateSpec(&spec.Spec, defaultResources)
}

// ValidateImagePullSecrets sets default values for imagePullSecrets if not provided
func ValidateImagePullSecrets(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ValidateImagePullSecrets").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// If no imagePullSecrets are configured
	var nilImagePullSecrets []corev1.LocalObjectReference
	if len(spec.ImagePullSecrets) == 0 {
		spec.ImagePullSecrets = nilImagePullSecrets
		return nil
	}

	// If configured, validated if the secret/s exist
	for _, secret := range spec.ImagePullSecrets {
		_, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), cr.GetName(), secret.Name)
		if err != nil {
			scopedLog.Error(err, "Couldn't get secret in the imagePullSecrets config", "Secret", secret.Name)
		}
	}

	return nil
}

// getSplunkDefaults returns a Kubernetes ConfigMap containing defaults for a Splunk Enterprise resource.
func getSplunkDefaults(identifier, namespace string, instanceType InstanceType, defaults string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkDefaultsName(identifier, instanceType),
			Namespace: namespace,
		},
		Data: map[string]string{
			"default.yml": defaults,
		},
	}
}

// getSplunkPorts returns a map of ports to use for Splunk instances.
func getSplunkPorts(instanceType InstanceType) map[string]int {
	result := map[string]int{
		GetPortName(splunkwebPort, protoHTTP): 8000,
		GetPortName(splunkdPort, protoHTTPS):  8089,
	}

	switch instanceType {
	case SplunkMonitoringConsole:
		result[GetPortName(hecPort, protoHTTP)] = 8088
		result[GetPortName(s2sPort, protoTCP)] = 9997
	case SplunkStandalone:
		result[GetPortName(hecPort, protoHTTP)] = 8088
		result[GetPortName(s2sPort, protoTCP)] = 9997
	case SplunkIndexer:
		result[GetPortName(hecPort, protoHTTP)] = 8088
		result[GetPortName(s2sPort, protoTCP)] = 9997
	}

	return result
}

// getSplunkContainerPorts returns a list of Kubernetes ContainerPort objects for Splunk instances.
func getSplunkContainerPorts(instanceType InstanceType) []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range getSplunkPorts(instanceType) {
		l = append(l, corev1.ContainerPort{
			Name:          key,
			ContainerPort: int32(value),
			Protocol:      corev1.ProtocolTCP,
		})
	}
	return l
}

// getSplunkServicePorts returns a list of Kubernetes ServicePort objects for Splunk instances.
func getSplunkServicePorts(instanceType InstanceType) []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range getSplunkPorts(instanceType) {
		l = append(l, corev1.ServicePort{
			Name:       key,
			Port:       int32(value),
			TargetPort: intstr.FromInt(value),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	return l
}

// addSplunkVolumeToTemplate modifies the podTemplateSpec object to incorporate an additional VolumeSource.
func addSplunkVolumeToTemplate(podTemplateSpec *corev1.PodTemplateSpec, name string, mountPath string, volumeSource corev1.VolumeSource) {
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, corev1.Volume{
		Name:         name,
		VolumeSource: volumeSource,
	})

	for idx := range podTemplateSpec.Spec.Containers {
		containerSpec := &podTemplateSpec.Spec.Containers[idx]
		containerSpec.VolumeMounts = append(containerSpec.VolumeMounts, corev1.VolumeMount{
			Name:      name,
			MountPath: mountPath,
		})
	}
}

// addPVCVolumes adds pvc volumes to statefulSet
func addPVCVolumes(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, statefulSet *appsv1.StatefulSet, labels map[string]string, volumeType string) error {
	// prepare and append persistent volume claims if storage is not ephemeral
	var err error
	volumeClaimTemplate, err := getSplunkVolumeClaims(cr, spec, labels, volumeType)
	if err != nil {
		return err
	}
	statefulSet.Spec.VolumeClaimTemplates = append(statefulSet.Spec.VolumeClaimTemplates, volumeClaimTemplate)

	// add volume mounts to splunk container for the PVCs
	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      volumeClaimTemplate.GetName(),
			MountPath: fmt.Sprintf(splcommon.SplunkMountDirecPrefix, volumeType),
		})

	return nil
}

// addEphemeralVolumes adds ephemeral volumes to statefulSet
func addEphemeralVolumes(statefulSet *appsv1.StatefulSet, volumeType string) error {
	// add ephemeral volumes to the splunk pod
	emptyVolumeSource := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}
	statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes,
		corev1.Volume{
			Name: fmt.Sprintf(splcommon.SplunkMountNamePrefix, volumeType), VolumeSource: emptyVolumeSource,
		})

	// add volume mounts to splunk container for the ephemeral volumes
	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      fmt.Sprintf(splcommon.SplunkMountNamePrefix, volumeType),
			MountPath: fmt.Sprintf(splcommon.SplunkMountDirecPrefix, volumeType),
		})

	return nil
}

// addStorageVolumes adds storage volumes to the StatefulSet
func addStorageVolumes(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, statefulSet *appsv1.StatefulSet, labels map[string]string) error {
	// configure storage for mount path /opt/splunk/etc
	if spec.EtcVolumeStorageConfig.EphemeralStorage {
		// add ephemeral volumes
		_ = addEphemeralVolumes(statefulSet, splcommon.EtcVolumeStorage)
	} else {
		// add PVC volumes
		err := addPVCVolumes(cr, spec, statefulSet, labels, splcommon.EtcVolumeStorage)
		if err != nil {
			return err
		}
	}

	// configure storage for mount path /opt/splunk/var
	if spec.VarVolumeStorageConfig.EphemeralStorage {
		// add ephemeral volumes
		_ = addEphemeralVolumes(statefulSet, splcommon.VarVolumeStorage)
	} else {
		// add PVC volumes
		err := addPVCVolumes(cr, spec, statefulSet, labels, splcommon.VarVolumeStorage)
		if err != nil {
			return err
		}
	}

	return nil
}

// getSplunkStatefulSet returns a Kubernetes StatefulSet object for Splunk instances configured for a Splunk Enterprise resource.
func getSplunkStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType InstanceType, replicas int32, extraEnv []corev1.EnvVar) (*appsv1.StatefulSet, error) {

	// prepare misc values
	ports := splcommon.SortContainerPorts(getSplunkContainerPorts(instanceType)) // note that port order is important for tests
	annotations := splcommon.GetIstioAnnotations(ports)
	selectLabels := getSplunkLabels(cr.GetName(), instanceType, spec.ClusterMasterRef.Name)
	if len(spec.ClusterManagerRef.Name) > 0 && len(spec.ClusterMasterRef.Name) == 0 {
		selectLabels = getSplunkLabels(cr.GetName(), instanceType, spec.ClusterManagerRef.Name)
	}
	affinity := splcommon.AppendPodAntiAffinity(&spec.Affinity, cr.GetName(), instanceType.ToString())

	// start with same labels as selector; note that this object gets modified by splcommon.AppendParentMeta()
	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      GetSplunkStatefulsetName(instanceType, cr.GetName()),
	}
	statefulSet := &appsv1.StatefulSet{}
	err := client.Get(ctx, namespacedName, statefulSet)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, err
	}

	if k8serrors.IsNotFound(err) {
		// create statefulset configuration
		statefulSet = &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{
				Kind:       "StatefulSet",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetSplunkStatefulsetName(instanceType, cr.GetName()),
				Namespace: cr.GetNamespace(),
			},
		}
	}

	statefulSet.Spec = appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: selectLabels,
		},
		ServiceName:         GetSplunkServiceName(instanceType, cr.GetName(), true),
		Replicas:            &replicas,
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.OnDeleteStatefulSetStrategyType,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				Affinity:         affinity,
				Tolerations:      spec.Tolerations,
				SchedulerName:    spec.SchedulerName,
				ImagePullSecrets: spec.ImagePullSecrets,
				Containers: []corev1.Container{
					{
						Image:           spec.Image,
						ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
						Name:            "splunk",
						Ports:           ports,
					},
				},
			},
		},
	}

	// Add storage volumes
	err = addStorageVolumes(cr, spec, statefulSet, labels)
	if err != nil {
		return statefulSet, err
	}

	// add serviceaccount if configured
	if spec.ServiceAccount != "" {
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: spec.ServiceAccount}
		_, err := splctrl.GetServiceAccount(ctx, client, namespacedName)
		if err == nil {
			// serviceAccount exists
			statefulSet.Spec.Template.Spec.ServiceAccountName = spec.ServiceAccount
		}
	}

	// append labels and annotations from parent
	splcommon.AppendParentMeta(statefulSet.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())

	// retrieve the secret to upload to the statefulSet pod
	statefulSetSecret, err := splutil.GetLatestVersionedSecret(ctx, client, cr, cr.GetNamespace(), statefulSet.GetName())
	if err != nil || statefulSetSecret == nil {
		return statefulSet, err
	}

	// update statefulset's pod template with common splunk pod config
	updateSplunkPodTemplateWithConfig(ctx, client, &statefulSet.Spec.Template, cr, spec, instanceType, extraEnv, statefulSetSecret.GetName())

	// make Splunk Enterprise object the owner
	statefulSet.SetOwnerReferences(append(statefulSet.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	return statefulSet, nil
}

// getSmartstoreConfigMap returns the smartstore configMap, if it exists and applicable for that instanceType
func getSmartstoreConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType) *corev1.ConfigMap {
	var configMap *corev1.ConfigMap

	if instanceType == SplunkStandalone || isCMDeployed(instanceType) {
		smartStoreConfigMapName := GetSplunkSmartstoreConfigMapName(cr.GetName(), cr.GetObjectKind().GroupVersionKind().Kind)
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: smartStoreConfigMapName}
		configMap, _ = splctrl.GetConfigMap(ctx, client, namespacedName)
	}

	return configMap
}

// updateSplunkPodTemplateWithConfig modifies the podTemplateSpec object based on configuration of the Splunk Enterprise resource.
func updateSplunkPodTemplateWithConfig(ctx context.Context, client splcommon.ControllerClient, podTemplateSpec *corev1.PodTemplateSpec, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType InstanceType, extraEnv []corev1.EnvVar, secretToMount string) {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updateSplunkPodTemplateWithConfig").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	// Add custom ports to splunk containers
	if spec.ServiceTemplate.Spec.Ports != nil {
		for idx := range podTemplateSpec.Spec.Containers {
			for _, p := range spec.ServiceTemplate.Spec.Ports {

				podTemplateSpec.Spec.Containers[idx].Ports = append(podTemplateSpec.Spec.Containers[idx].Ports, corev1.ContainerPort{
					Name:          p.Name,
					ContainerPort: int32(p.TargetPort.IntValue()),
					Protocol:      p.Protocol,
				})
			}
		}
	}

	// Add custom volumes to splunk containers other than MC(where CR spec volumes are not needed)
	if spec.Volumes != nil {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, spec.Volumes...)
		for idx := range podTemplateSpec.Spec.Containers {
			for v := range spec.Volumes {
				podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(podTemplateSpec.Spec.Containers[idx].VolumeMounts, corev1.VolumeMount{
					Name:      spec.Volumes[v].Name,
					MountPath: "/mnt/" + spec.Volumes[v].Name,
				})
			}
		}
	}

	// Explicitly set the default value here so we can compare for changes correctly with current statefulset.
	secretVolDefaultMode := int32(corev1.SecretVolumeSourceDefaultMode)
	addSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-secrets", "/mnt/splunk-secrets", corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName:  secretToMount,
			DefaultMode: &secretVolDefaultMode,
		},
	})

	// Explicitly set the default value here so we can compare for changes correctly with current statefulset.
	configMapVolDefaultMode := int32(corev1.ConfigMapVolumeSourceDefaultMode)

	// add inline defaults to all splunk containers other than MC(where CR spec defaults are not needed)
	if spec.Defaults != "" {
		configMapName := GetSplunkDefaultsName(cr.GetName(), instanceType)
		addSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-defaults", "/mnt/splunk-defaults", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
				DefaultMode: &configMapVolDefaultMode,
			},
		})

		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

		// We will update the annotation for resource version in the pod template spec
		// so that any change in the ConfigMap will lead to recycle of the pod.
		configMapResourceVersion, err := splctrl.GetConfigMapResourceVersion(ctx, client, namespacedName)
		if err == nil {
			podTemplateSpec.ObjectMeta.Annotations["defaultConfigRev"] = configMapResourceVersion
		} else {
			scopedLog.Error(err, "Updation of default configMap annotation failed")
		}
	}

	smartstoreConfigMap := getSmartstoreConfigMap(ctx, client, cr, instanceType)
	if smartstoreConfigMap != nil {
		addSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-operator", "/mnt/splunk-operator/local/", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: smartstoreConfigMap.GetName(),
				},
				DefaultMode: &configMapVolDefaultMode,
				Items: []corev1.KeyToPath{
					{Key: "indexes.conf", Path: "indexes.conf", Mode: &configMapVolDefaultMode},
					{Key: "server.conf", Path: "server.conf", Mode: &configMapVolDefaultMode},
					{Key: configToken, Path: configToken, Mode: &configMapVolDefaultMode},
				},
			},
		})

		// 1. For Indexer cluster case, do not set the annotation on CM pod. smartstore config is
		// propagated through the CM manager apps bundle push
		// 2. In case of Standalone, reset the Pod, by updating the latest Resource version of the
		// smartstore config map.
		if instanceType == SplunkStandalone {
			podTemplateSpec.ObjectMeta.Annotations[smartStoreConfigRev] = smartstoreConfigMap.ResourceVersion
		}
	}

	// update security context
	runAsUser := int64(41812)
	fsGroup := int64(41812)
	runAsNonRoot := true
	podTemplateSpec.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser:    &runAsUser,
		FSGroup:      &fsGroup,
		RunAsNonRoot: &runAsNonRoot,
	}

	var additionalDelayForAppInstallation int32

	livenessProbe := getLivenessProbe(ctx, cr, instanceType, spec, additionalDelayForAppInstallation)
	readinessProbe := getReadinessProbe(ctx, cr, instanceType, spec, 0)

	// prepare defaults variable
	splunkDefaults := "/mnt/splunk-secrets/default.yml"
	// Check for apps defaults and add it to only the standalone or deployer/cm/mc instances
	if spec.DefaultsURLApps != "" && instanceType != SplunkIndexer && instanceType != SplunkSearchHead {
		splunkDefaults = fmt.Sprintf("%s,%s", spec.DefaultsURLApps, splunkDefaults)
	}
	if spec.DefaultsURL != "" {
		splunkDefaults = fmt.Sprintf("%s,%s", spec.DefaultsURL, splunkDefaults)
	}
	if spec.Defaults != "" {
		splunkDefaults = fmt.Sprintf("%s,%s", "/mnt/splunk-defaults/default.yml", splunkDefaults)
	}

	// prepare container env variables
	role := instanceType.ToRole()
	if instanceType == SplunkStandalone && (len(spec.ClusterMasterRef.Name) > 0 || len(spec.ClusterManagerRef.Name) > 0) {
		role = SplunkSearchHead.ToRole()
	}
	env := []corev1.EnvVar{
		{Name: "SPLUNK_HOME", Value: "/opt/splunk"},
		{Name: "SPLUNK_START_ARGS", Value: "--accept-license"},
		{Name: "SPLUNK_DEFAULTS_URL", Value: splunkDefaults},
		{Name: "SPLUNK_HOME_OWNERSHIP_ENFORCEMENT", Value: "false"},
		{Name: "SPLUNK_ROLE", Value: role},
		{Name: "SPLUNK_DECLARATIVE_ADMIN_PASSWORD", Value: "true"},
	}

	// update variables for licensing, if configured
	if spec.LicenseURL != "" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_LICENSE_URI",
			Value: spec.LicenseURL,
		})
	}
	if instanceType != SplunkLicenseManager && spec.LicenseManagerRef.Name != "" {
		licenseManagerURL := GetSplunkServiceName(SplunkLicenseManager, spec.LicenseManagerRef.Name, false)
		if spec.LicenseManagerRef.Namespace != "" {
			licenseManagerURL = splcommon.GetServiceFQDN(spec.LicenseManagerRef.Namespace, licenseManagerURL)
		}
		env = append(env, corev1.EnvVar{
			Name:  splcommon.LicenseManagerURL,
			Value: licenseManagerURL,
		})
	} else if instanceType != SplunkLicenseMaster && spec.LicenseMasterRef.Name != "" {
		licenseMasterURL := GetSplunkServiceName(SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
		if spec.LicenseMasterRef.Namespace != "" {
			licenseMasterURL = splcommon.GetServiceFQDN(spec.LicenseMasterRef.Namespace, licenseMasterURL)
		}
		env = append(env, corev1.EnvVar{
			Name:  splcommon.LicenseManagerURL,
			Value: licenseMasterURL,
		})
	}

	// append URL for cluster manager, if configured
	var clusterManagerURL string
	if isCMDeployed(instanceType) {
		// This makes splunk-ansible configure indexer-discovery on cluster-manager
		clusterManagerURL = "localhost"
	} else if spec.ClusterManagerRef.Name != "" {
		clusterManagerURL = GetSplunkServiceName(SplunkClusterManager, spec.ClusterManagerRef.Name, false)
		if spec.ClusterManagerRef.Namespace != "" {
			clusterManagerURL = splcommon.GetServiceFQDN(spec.ClusterManagerRef.Namespace, clusterManagerURL)
		}
		if spec.LicenseManagerRef.Name == "" || spec.LicenseMasterRef.Name == "" {
			//Check if CM is connected to a LicenseManager
			namespacedName := types.NamespacedName{
				Namespace: cr.GetNamespace(),
				Name:      spec.ClusterManagerRef.Name,
			}
			managerIdxCluster := &enterpriseApi.ClusterManager{}
			err := client.Get(ctx, namespacedName, managerIdxCluster)
			if err != nil {
				scopedLog.Error(err, "Unable to get ClusterManager")
			}

			if managerIdxCluster.Spec.LicenseManagerRef.Name != "" {
				licenseManagerURL := GetSplunkServiceName(SplunkLicenseManager, managerIdxCluster.Spec.LicenseManagerRef.Name, false)
				if managerIdxCluster.Spec.LicenseManagerRef.Namespace != "" {
					licenseManagerURL = splcommon.GetServiceFQDN(managerIdxCluster.Spec.LicenseManagerRef.Namespace, licenseManagerURL)
				}
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseManagerURL,
				})
			} else if managerIdxCluster.Spec.LicenseMasterRef.Name != "" {
				licenseMasterURL := GetSplunkServiceName(SplunkLicenseMaster, managerIdxCluster.Spec.LicenseMasterRef.Name, false)
				if managerIdxCluster.Spec.LicenseMasterRef.Namespace != "" {
					licenseMasterURL = splcommon.GetServiceFQDN(managerIdxCluster.Spec.LicenseMasterRef.Namespace, licenseMasterURL)
				}
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseMasterURL,
				})
			}
		}
	} else if spec.ClusterMasterRef.Name != "" {
		clusterManagerURL = GetSplunkServiceName(SplunkClusterMaster, spec.ClusterMasterRef.Name, false)
		if spec.ClusterMasterRef.Namespace != "" {
			clusterManagerURL = splcommon.GetServiceFQDN(spec.ClusterMasterRef.Namespace, clusterManagerURL)
		}
		if spec.LicenseManagerRef.Name == "" || spec.LicenseMasterRef.Name == "" {
			//Check if CM is connected to a LicenseManager
			namespacedName := types.NamespacedName{
				Namespace: cr.GetNamespace(),
				Name:      spec.ClusterMasterRef.Name,
			}
			managerIdxCluster := &enterpriseApiV3.ClusterMaster{}
			err := client.Get(ctx, namespacedName, managerIdxCluster)
			if err != nil {
				scopedLog.Error(err, "Unable to get ClusterManager")
			}

			if managerIdxCluster.Spec.LicenseManagerRef.Name != "" {
				licenseManagerURL := GetSplunkServiceName(SplunkLicenseManager, managerIdxCluster.Spec.LicenseManagerRef.Name, false)
				if managerIdxCluster.Spec.LicenseManagerRef.Namespace != "" {
					licenseManagerURL = splcommon.GetServiceFQDN(managerIdxCluster.Spec.LicenseManagerRef.Namespace, licenseManagerURL)
				}
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseManagerURL,
				})
			} else if managerIdxCluster.Spec.LicenseMasterRef.Name != "" {
				licenseMasterURL := GetSplunkServiceName(SplunkLicenseMaster, managerIdxCluster.Spec.LicenseMasterRef.Name, false)
				if managerIdxCluster.Spec.LicenseMasterRef.Namespace != "" {
					licenseMasterURL = splcommon.GetServiceFQDN(managerIdxCluster.Spec.LicenseMasterRef.Namespace, licenseMasterURL)
				}
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseMasterURL,
				})
			}
		}
	}

	if clusterManagerURL != "" {
		extraEnv = append(extraEnv, corev1.EnvVar{
			Name:  splcommon.ClusterManagerURL,
			Value: clusterManagerURL,
		})
	}

	// append REF for monitoring console if configured
	if spec.MonitoringConsoleRef.Name != "" {
		extraEnv = append(extraEnv, corev1.EnvVar{
			Name:  "SPLUNK_MONITORING_CONSOLE_REF",
			Value: spec.MonitoringConsoleRef.Name,
		})
	}

	// Add extraEnv from the CommonSplunkSpec config to the extraEnv variable list
	for _, envVar := range spec.ExtraEnv {
		extraEnv = append(extraEnv, envVar)
	}

	// append any extra variables
	env = append(env, extraEnv...)

	// update each container in pod
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].Resources = spec.Resources
		podTemplateSpec.Spec.Containers[idx].LivenessProbe = livenessProbe
		podTemplateSpec.Spec.Containers[idx].ReadinessProbe = readinessProbe
		podTemplateSpec.Spec.Containers[idx].Env = env
	}
}

// getLivenessProbe the probe for checking the liveness of the Pod
// uses script provided by enterprise container to check if pod is alive
func getLivenessProbe(ctx context.Context, cr splcommon.MetaObject, instanceType InstanceType, spec *enterpriseApi.CommonSplunkSpec, additionalDelay int32) *corev1.Probe {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getLivenessProbe").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	livenessDelay := int32(livenessProbeDefaultDelaySec)

	// If configured, always use the Liveness initial delay from the CR
	if spec.LivenessInitialDelaySeconds != 0 {
		livenessDelay = spec.LivenessInitialDelaySeconds
	} else {
		livenessDelay += additionalDelay
	}
	scopedLog.Info("LivenessProbeInitialDelay", "configured", spec.LivenessInitialDelaySeconds, "additionalDelay", additionalDelay, "finalCalculatedValue", livenessDelay)

	livenessCommand := []string{
		"/sbin/checkstate.sh",
	}

	return getProbe(livenessCommand, livenessDelay, livenessProbeTimeoutSec, livenessProbePeriodSec)
}

// getReadinessProbe provides the probe for checking the readiness of the Pod
// pod is ready if container artifact file is created with contents of "started".
func getReadinessProbe(ctx context.Context, cr splcommon.MetaObject, instanceType InstanceType, spec *enterpriseApi.CommonSplunkSpec, additionalDelay int32) *corev1.Probe {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getReadinessProbe")

	readinessDelay := int32(readinessProbeDefaultDelaySec)

	// If configured, always use the readiness initial delay from the CR
	if spec.ReadinessInitialDelaySeconds != 0 {
		readinessDelay = spec.ReadinessInitialDelaySeconds
	} else {
		readinessDelay += additionalDelay
	}

	scopedLog.Info("ReadinessProbeInitialDelay", "configured", spec.ReadinessInitialDelaySeconds, "additionalDelay", additionalDelay, "finalCalculatedValue", readinessDelay)

	readinessCommand := []string{
		"/bin/grep",
		"started",
		"/opt/container_artifact/splunk-container.state",
	}

	return getProbe(readinessCommand, readinessDelay, readinessProbeTimeoutSec, readinessProbePeriodSec)
}

// getProbe returns the Probe for given values.
func getProbe(command []string, delay, timeout, period int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: command,
			},
		},
		InitialDelaySeconds: delay,
		TimeoutSeconds:      timeout,
		PeriodSeconds:       period,
	}
}

// getVolumeSourceMountFromConfigMapData returns a volume source with the configMap Data entries
func getVolumeSourceMountFromConfigMapData(configMap *corev1.ConfigMap, mode *int32) corev1.VolumeSource {
	volumeSource := corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: configMap.GetName(),
			},
			DefaultMode: mode,
		},
	}

	for key := range configMap.Data {
		volumeSource.ConfigMap.Items = append(volumeSource.ConfigMap.Items, corev1.KeyToPath{Key: key, Path: key, Mode: mode})
	}
	//  Map traversal order is not guaranteed. Always sort the slice to avoid (random) pod resets due to the ordering
	splcommon.SortSlice(volumeSource.ConfigMap.Items, splcommon.SortFieldKey)

	return volumeSource
}

// isSmartstoreEnabled checks and returns true if smartstore is configured
func isSmartstoreConfigured(smartstore *enterpriseApi.SmartStoreSpec) bool {
	if smartstore == nil {
		return false
	}

	return smartstore.IndexList != nil || smartstore.VolList != nil || smartstore.Defaults.VolName != ""
}

func isCMDeployed(instanceType InstanceType) bool {
	return instanceType == SplunkClusterManager || instanceType == SplunkClusterMaster
}

// AreRemoteVolumeKeysChanged discovers if the S3 keys changed
func AreRemoteVolumeKeysChanged(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType, smartstore *enterpriseApi.SmartStoreSpec, ResourceRev map[string]string, retError *error) bool {
	// No need to proceed if the smartstore is not configured
	if !isSmartstoreConfigured(smartstore) {
		return false
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AreRemoteVolumeKeysChanged").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	volList := smartstore.VolList
	for _, volume := range volList {
		if volume.SecretRef != "" {
			namespaceScopedSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), cr.GetName(), volume.SecretRef)
			// Ideally, this should have been detected in Spec validation time
			if err != nil {
				*retError = fmt.Errorf("Not able to access secret object = %s, reason: %s", volume.SecretRef, err)
				return false
			}

			// Check if the secret version is already tracked, and if there is a change in it
			if existingSecretVersion, ok := ResourceRev[volume.SecretRef]; ok {
				if existingSecretVersion != namespaceScopedSecret.ResourceVersion {
					scopedLog.Info("Secret Keys changed", "Previous Resource Version", existingSecretVersion, "Current Version", namespaceScopedSecret.ResourceVersion)
					ResourceRev[volume.SecretRef] = namespaceScopedSecret.ResourceVersion
					return true
				}
				return false
			}

			// First time adding to track the secret resource version
			ResourceRev[volume.SecretRef] = namespaceScopedSecret.ResourceVersion
		} else {
			scopedLog.Info("No valid SecretRef for volume.  No secret to track.", "volumeName", volume.Name)
		}
	}

	return false
}

// ApplyManualAppUpdateConfigMap applies the manual app update config map
func ApplyManualAppUpdateConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, crKindMap map[string]string) (*corev1.ConfigMap, error) {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyManualAppUpdateConfigMap").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	var configMap *corev1.ConfigMap
	var err error
	var newConfigMap bool
	configMap, err = splctrl.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		configMap = splctrl.PrepareConfigMap(configMapName, cr.GetNamespace(), crKindMap)
		newConfigMap = true
	}

	configMap.Data = crKindMap

	// set this CR as owner reference for the configMap
	configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	if newConfigMap {
		scopedLog.Info("Creating manual app update configMap")
		err = splutil.CreateResource(ctx, client, configMap)
		if err != nil {
			scopedLog.Error(err, "Unable to create the configMap", "name", configMapName)
			return configMap, err
		}
	} else {
		scopedLog.Info("Updating manual app update configMap")
		err = splutil.UpdateResource(ctx, client, configMap)
		if err != nil {
			scopedLog.Error(err, "Unable to update the configMap", "name", configMapName)
			return configMap, err
		}
	}
	return configMap, nil
}

// getManualUpdateStatus extracts the status field from the configMap data
func getManualUpdateStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) string {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getManualUpdateStatus").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := splctrl.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get the configMap", "name", configMapName)
		return ""
	}

	statusRegex := ".*status: (?P<status>.*).*"
	data := configMap.Data[cr.GetObjectKind().GroupVersionKind().Kind]

	return extractFieldFromConfigMapData(statusRegex, data)
}

// getManualUpdateRefCount extracts the refCount field from the configMap data
func getManualUpdateRefCount(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) int {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getManualUpdateRefCount").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	var refCount int
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := splctrl.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get the configMap", "name", configMapName)
		return refCount
	}

	refCountRegex := ".*refCount: (?P<refCount>.*).*"
	data := configMap.Data[cr.GetObjectKind().GroupVersionKind().Kind]

	refCount, _ = strconv.Atoi(extractFieldFromConfigMapData(refCountRegex, data))
	return refCount
}

// createOrUpdateAppUpdateConfigMap creates or updates the manual app update configMap
func createOrUpdateAppUpdateConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) (*corev1.ConfigMap, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("createOrUpdateAppUpdateConfigMap").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	var crKindMap map[string]string
	var configMapData, status string
	var configMap *corev1.ConfigMap
	var err error
	var numOfObjects int

	kind := cr.GetObjectKind().GroupVersionKind().Kind

	configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	mux := getResourceMutex(configMapName)
	mux.Lock()
	defer mux.Unlock()
	configMap, err = splctrl.GetConfigMap(ctx, client, namespacedName)
	if err == nil {
		// If this CR is already an owner reference, then do nothing.
		// This can happen if we have already set this CR as ownerRef in the first time,
		// and we reach here again during the next reconcile.
		currentOwnerRef := configMap.GetOwnerReferences()
		for i := 0; i < len(currentOwnerRef); i++ {
			if reflect.DeepEqual(currentOwnerRef[i], splcommon.AsOwner(cr, false)) {
				return configMap, nil
			}
		}

		scopedLog.Info("Existing configMap data", "data", configMap.Data)
		crKindMap = configMap.Data

		// get the number of instance types of this kind
		numOfObjects = getNumOfOwnerRefsKind(configMap, kind)
	}

	// prepare the configMap data OR
	// initialize the configMap data for this CR type,
	// if it did not exist before
	if crKindMap == nil {
		crKindMap = make(map[string]string)
	}
	if _, ok := crKindMap[kind]; !ok {
		status = "off"
	} else {
		status = getManualUpdateStatus(ctx, client, cr, configMapName)
	}

	configMapData = fmt.Sprintf(`status: %s
refCount: %d`, status, numOfObjects+1)
	crKindMap[kind] = configMapData

	// Create/update the configMap to store the values of manual trigger per CR kind.
	configMap, err = ApplyManualAppUpdateConfigMap(ctx, client, cr, crKindMap)
	if err != nil {
		scopedLog.Error(err, "Create/update configMap for app update failed")
		return configMap, err
	}

	return configMap, nil
}

// initAppFrameWorkContext used to initialize the appframework context
func initAppFrameWorkContext(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkConf *enterpriseApi.AppFrameworkSpec, appStatusContext *enterpriseApi.AppDeploymentContext) error {
	if appStatusContext.AppsSrcDeployStatus == nil {
		appStatusContext.AppsSrcDeployStatus = make(map[string]enterpriseApi.AppSrcDeployInfo)
		//Note:- Set version only at the time of allocating AppsSrcDeployStatus. This is important, so that we don't
		// interfere with the upgrade scenarios. So, if the AppsSrcDeployStatus is already allocated
		// and the version is not `CurrentAfwVersion`, means it is migration scenario, and the migration logic should
		// handle upgrading to the latest version.
		appStatusContext.Version = enterpriseApi.LatestAfwVersion

		_, err := createOrUpdateAppUpdateConfigMap(ctx, client, cr)
		if err != nil {
			return err
		}
	}

	for _, vol := range appFrameworkConf.VolList {
		if _, ok := splclient.S3Clients[vol.Provider]; !ok {
			splclient.RegisterS3Client(ctx, vol.Provider)
		}
	}
	return nil
}

// getAppSrcScope returns the scope of a given appSource
func getAppSrcScope(ctx context.Context, appFrameworkConf *enterpriseApi.AppFrameworkSpec, appSrcName string) string {
	for _, appSrc := range appFrameworkConf.AppSources {
		if appSrc.Name == appSrcName {
			if appSrc.Scope != "" {
				return appSrc.Scope
			}

			break
		}
	}

	return appFrameworkConf.Defaults.Scope
}

// getAppSrcSpec returns AppSourceSpec from the app source name
func getAppSrcSpec(appSources []enterpriseApi.AppSourceSpec, appSrcName string) (*enterpriseApi.AppSourceSpec, error) {
	var err error

	for _, appSrc := range appSources {
		if appSrc.Name == appSrcName {
			return &appSrc, err
		}
	}

	err = fmt.Errorf("unable to find app source spec for app source: %s", appSrcName)
	return nil, err
}

// CheckIfAppSrcExistsInConfig returns if the given appSource is available in the configuration or not
func CheckIfAppSrcExistsInConfig(appFrameworkConf *enterpriseApi.AppFrameworkSpec, appSrcName string) bool {
	for _, appSrc := range appFrameworkConf.AppSources {
		if appSrc.Name == appSrcName {
			return true
		}
	}
	return false
}

// isAppSourceScopeValid checks for valid app source
func isAppSourceScopeValid(scope string) bool {
	return scope == enterpriseApi.ScopeLocal || scope == enterpriseApi.ScopeCluster || scope == enterpriseApi.ScopeClusterWithPreConfig
}

// validateSplunkAppSources validates the App source config in App Framework spec
func validateSplunkAppSources(appFramework *enterpriseApi.AppFrameworkSpec, localScope bool) error {

	duplicateAppSourceStorageChecker := make(map[string]map[string]bool)
	duplicateAppSourceStorageChecker[enterpriseApi.ScopeLocal] = make(map[string]bool)
	if !localScope {
		duplicateAppSourceStorageChecker[enterpriseApi.ScopeCluster] = make(map[string]bool)
		duplicateAppSourceStorageChecker[enterpriseApi.ScopeClusterWithPreConfig] = make(map[string]bool)
	}

	duplicateAppSourceNameChecker := make(map[string]bool)

	var vol string

	// Make sure that all the App Sources are provided with the mandatory config values.
	for i, appSrc := range appFramework.AppSources {
		if appSrc.Name == "" {
			return fmt.Errorf("app Source name is missing for AppSource at: %d", i)
		}

		if _, ok := duplicateAppSourceNameChecker[appSrc.Name]; ok {
			return fmt.Errorf("multiple app sources with the name %s is not allowed", appSrc.Name)
		}
		duplicateAppSourceNameChecker[appSrc.Name] = true

		if appSrc.Location == "" {
			return fmt.Errorf("app Source location is missing for AppSource: %s", appSrc.Name)
		}

		if appSrc.VolName != "" {
			_, err := splclient.CheckIfVolumeExists(appFramework.VolList, appSrc.VolName)
			if err != nil {
				return fmt.Errorf("invalid Volume Name for App Source: %s. %s", appSrc.Name, err)
			}
			vol = appSrc.VolName
		} else {
			if appFramework.Defaults.VolName == "" {
				return fmt.Errorf("volumeName is missing for App Source: %s", appSrc.Name)
			}
			vol = appFramework.Defaults.VolName
		}

		var scope string
		if appSrc.Scope != "" {
			if localScope && appSrc.Scope != enterpriseApi.ScopeLocal {
				return fmt.Errorf("invalid scope for App Source: %s. Only local scope is supported for this kind of CR", appSrc.Name)
			}

			if !isAppSourceScopeValid(appSrc.Scope) {
				return fmt.Errorf("scope for App Source: %s should be either local or cluster or clusterWithPreConfig", appSrc.Name)
			}

			scope = appSrc.Scope
		} else {
			if appFramework.Defaults.Scope == "" {
				return fmt.Errorf("app Source scope is missing for: %s", appSrc.Name)
			}

			scope = appFramework.Defaults.Scope
		}

		if _, ok := duplicateAppSourceStorageChecker[scope][vol+appSrc.Location]; ok {
			return fmt.Errorf("duplicate App Source configured for Volume: %s, and Location: %s combo. Remove the duplicate entry and reapply the configuration", vol, appSrc.Location)
		}
		duplicateAppSourceStorageChecker[scope][vol+appSrc.Location] = true
	}

	if localScope && appFramework.Defaults.Scope != "" && appFramework.Defaults.Scope != enterpriseApi.ScopeLocal {
		return fmt.Errorf("invalid scope for defaults config. Only local scope is supported for this kind of CR")
	}

	if appFramework.Defaults.Scope != "" && !isAppSourceScopeValid(appFramework.Defaults.Scope) {
		return fmt.Errorf("scope for defaults should be either local Or cluster, but configured as: %s", appFramework.Defaults.Scope)
	}

	if appFramework.Defaults.VolName != "" {
		_, err := splclient.CheckIfVolumeExists(appFramework.VolList, appFramework.Defaults.VolName)
		if err != nil {
			return fmt.Errorf("invalid Volume Name for Defaults. Error: %s", err)
		}
	}

	return nil
}

//  isAppFrameworkConfigured checks and returns true if App Framework is configured
//  App Repo config without any App sources will not cause any App Framework activity
func isAppFrameworkConfigured(appFramework *enterpriseApi.AppFrameworkSpec) bool {
	return !(appFramework == nil || appFramework.AppSources == nil)
}

// ValidateAppFrameworkSpec checks and validates the Apps Frame Work config
func ValidateAppFrameworkSpec(ctx context.Context, appFramework *enterpriseApi.AppFrameworkSpec, appContext *enterpriseApi.AppDeploymentContext, localScope bool) error {
	var err error
	if !isAppFrameworkConfigured(appFramework) {
		return nil
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ValidateAppFrameworkSpec")

	scopedLog.Info("configCheck", "scope", localScope)

	// Set the value in status field to be same as that in spec.
	appContext.AppsRepoStatusPollInterval = appFramework.AppsRepoPollInterval
	appContext.AppsStatusMaxConcurrentAppDownloads = appFramework.MaxConcurrentAppDownloads

	if appContext.AppsRepoStatusPollInterval <= 0 {
		scopedLog.Error(err, "appsRepoPollIntervalSeconds is not configured. Disabling polling of apps repo changes, defaulting to manual updates")
		appContext.AppsRepoStatusPollInterval = 0
	} else if appFramework.AppsRepoPollInterval < splcommon.MinAppsRepoPollInterval {
		scopedLog.Error(err, "configured appsRepoPollIntervalSeconds is too small", "configured value", appFramework.AppsRepoPollInterval, "Setting it to the default min. value(seconds)", splcommon.MinAppsRepoPollInterval)
		appContext.AppsRepoStatusPollInterval = splcommon.MinAppsRepoPollInterval
	} else if appFramework.AppsRepoPollInterval > splcommon.MaxAppsRepoPollInterval {
		scopedLog.Error(err, "configured appsRepoPollIntervalSeconds is too large", "configured value", appFramework.AppsRepoPollInterval, "Setting it to the default max. value(seconds)", splcommon.MaxAppsRepoPollInterval, "seconds", nil)
		appContext.AppsRepoStatusPollInterval = splcommon.MaxAppsRepoPollInterval
	}

	if appContext.AppsStatusMaxConcurrentAppDownloads <= 0 {
		scopedLog.Info("Invalid value of maxConcurrentAppDownloads", "configured value", appContext.AppsStatusMaxConcurrentAppDownloads, "Setting it to default value", splcommon.DefaultMaxConcurrentAppDownloads)
		appContext.AppsStatusMaxConcurrentAppDownloads = splcommon.DefaultMaxConcurrentAppDownloads
	}

	appDownloadVolume := splcommon.AppDownloadVolume
	_, err = os.Stat(appDownloadVolume)

	// check whether the temporary volume to download apps is mounted or not on the operator pod
	if _, err := os.Stat(appDownloadVolume); os.IsNotExist(err) {
		scopedLog.Error(err, "Volume needs to be mounted on operator pod to download apps. Please mount it as a separate volume on operator pod.", "volume path", appDownloadVolume)
		return err
	}

	err = validateRemoteVolumeSpec(ctx, appFramework.VolList, true)
	if err != nil {
		return err
	}

	err = validateSplunkAppSources(appFramework, localScope)

	if err == nil {
		scopedLog.Info("App framework configuration is valid")
	}
	return err
}

// validateRemoteVolumeSpec validates the Remote storage volume spec
func validateRemoteVolumeSpec(ctx context.Context, volList []enterpriseApi.VolumeSpec, isAppFramework bool) error {

	duplicateChecker := make(map[string]bool)

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("validateRemoteVolumeSpec")

	// Make sure that all the Volumes are provided with the mandatory config values.
	for i, volume := range volList {
		if _, ok := duplicateChecker[volume.Name]; ok {
			return fmt.Errorf("duplicate volume name detected: %s. Remove the duplicate entry and reapply the configuration", volume.Name)
		}
		duplicateChecker[volume.Name] = true
		// Make sure that the smartstore volume info is correct
		if volume.Name == "" {
			return fmt.Errorf("volume name is missing for volume at : %d", i)
		}
		if volume.Endpoint == "" {
			return fmt.Errorf("volume Endpoint URI is missing")
		}
		if volume.Path == "" {
			return fmt.Errorf("volume Path is missing")
		}
		// Make the secretRef optional if theyre using IAM roles
		if volume.SecretRef == "" {
			scopedLog.Info("No valid SecretRef for volume.", "volumeName", volume.Name)
		}

		// provider is used in App framework to pick the S3 client(aws, minio), and is not applicable to Smartstore
		// For now, Smartstore supports only S3, which is by default.
		if isAppFramework {
			if !isValidStorageType(volume.Type) {
				return fmt.Errorf("remote volume type is invalid. Only storageType=s3 is supported")
			}

			if !isValidProvider(volume.Provider) {
				return fmt.Errorf("s3 Provider is invalid")
			}
		}
	}
	return nil
}

// isValidStorageType checks if the storage type specified is valid and supported
func isValidStorageType(storage string) bool {
	return storage != "" && storage == "s3"
}

// isValidProvider checks if the provider specified is valid and supported
func isValidProvider(provider string) bool {
	return provider != "" && (provider == "aws" || provider == "minio")
}

// validateSplunkIndexesSpec validates the smartstore index spec
func validateSplunkIndexesSpec(smartstore *enterpriseApi.SmartStoreSpec) error {

	duplicateChecker := make(map[string]bool)

	// Make sure that all the indexes are provided with the mandatory config values.
	for i, index := range smartstore.IndexList {
		if index.Name == "" {
			return fmt.Errorf("index name is missing for index at: %d", i)
		}

		if _, ok := duplicateChecker[index.Name]; ok {
			return fmt.Errorf("duplicate index name detected: %s.Remove the duplicate entry and reapply the configuration", index.Name)
		}
		duplicateChecker[index.Name] = true
		if index.VolName == "" && smartstore.Defaults.VolName == "" {
			return fmt.Errorf("volumeName is missing for index: %s", index.Name)
		}

		if index.VolName != "" {
			_, err := splclient.CheckIfVolumeExists(smartstore.VolList, index.VolName)
			if err != nil {
				return fmt.Errorf("invalid configuration for index: %s. %s", index.Name, err)
			}
		}
	}

	return nil
}

// ValidateSplunkSmartstoreSpec checks and validates the smartstore config
func ValidateSplunkSmartstoreSpec(ctx context.Context, smartstore *enterpriseApi.SmartStoreSpec) error {
	var err error

	// Smartstore is an optional config (at least) for now
	if !isSmartstoreConfigured(smartstore) {
		return nil
	}

	numVolumes := len(smartstore.VolList)
	numIndexes := len(smartstore.IndexList)
	if numIndexes > 0 && numVolumes == 0 {
		return fmt.Errorf("volume configuration is missing. Num. of indexes = %d. Num. of Volumes = %d", numIndexes, numVolumes)
	}

	err = validateRemoteVolumeSpec(ctx, smartstore.VolList, false)
	if err != nil {
		return err
	}

	defaults := smartstore.Defaults
	// When volName is configured, bucket remote path should also be configured
	if defaults.VolName != "" {
		_, err = splclient.CheckIfVolumeExists(smartstore.VolList, defaults.VolName)
		if err != nil {
			return fmt.Errorf("invalid configuration for defaults volume. %s", err)
		}
	}

	err = validateSplunkIndexesSpec(smartstore)
	return err
}

// GetSmartstoreVolumesConfig returns the list of Volumes configuration in INI format
func GetSmartstoreVolumesConfig(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec, mapData map[string]string) (string, error) {
	var volumesConf string

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetSmartstoreVolumesConfig")

	volumes := smartstore.VolList
	for i := 0; i < len(volumes); i++ {
		if volumes[i].SecretRef != "" {
			s3AccessKey, s3SecretKey, _, err := GetSmartstoreRemoteVolumeSecrets(ctx, volumes[i], client, cr, smartstore)
			if err != nil {
				return "", fmt.Errorf("Unable to read the secrets for volume = %s. %s", volumes[i].Name, err)
			}

			volumesConf = fmt.Sprintf(`%s
[volume:%s]
storageType = remote
path = s3://%s
remote.s3.access_key = %s
remote.s3.secret_key = %s
remote.s3.endpoint = %s
`, volumesConf, volumes[i].Name, volumes[i].Path, s3AccessKey, s3SecretKey, volumes[i].Endpoint)
		} else {
			scopedLog.Info("No valid secretRef configured.  Configure volume without access/secret keys", "volumeName", volumes[i].Name)
			volumesConf = fmt.Sprintf(`%s
[volume:%s]
storageType = remote
path = s3://%s
remote.s3.endpoint = %s
`, volumesConf, volumes[i].Name, volumes[i].Path, volumes[i].Endpoint)
		}
	}

	return volumesConf, nil
}

// GetSmartstoreIndexesConfig returns the list of indexes configuration in INI format
func GetSmartstoreIndexesConfig(indexes []enterpriseApi.IndexSpec) string {

	var indexesConf string

	defaultRemotePath := "$_index_name"

	for i := 0; i < len(indexes); i++ {
		// Write the index stanza name
		indexesConf = fmt.Sprintf(`%s
[%s]`, indexesConf, indexes[i].Name)

		if indexes[i].RemotePath != "" && indexes[i].VolName != "" {
			indexesConf = fmt.Sprintf(`%s
remotePath = volume:%s/%s`, indexesConf, indexes[i].VolName, indexes[i].RemotePath)
		} else if indexes[i].VolName != "" {
			indexesConf = fmt.Sprintf(`%s
remotePath = volume:%s/%s`, indexesConf, indexes[i].VolName, defaultRemotePath)
		}

		if indexes[i].HotlistBloomFilterRecencyHours != 0 {
			indexesConf = fmt.Sprintf(`%s
hotlist_bloom_filter_recency_hours = %d`, indexesConf, indexes[i].HotlistBloomFilterRecencyHours)
		}

		if indexes[i].HotlistRecencySecs != 0 {
			indexesConf = fmt.Sprintf(`%s
hotlist_recency_secs = %d`, indexesConf, indexes[i].HotlistRecencySecs)
		}

		if indexes[i].MaxGlobalDataSizeMB != 0 {
			indexesConf = fmt.Sprintf(`%s
maxGlobalDataSizeMB = %d`, indexesConf, indexes[i].MaxGlobalDataSizeMB)
		}

		if indexes[i].MaxGlobalRawDataSizeMB != 0 {
			indexesConf = fmt.Sprintf(`%s
maxGlobalRawDataSizeMB = %d`, indexesConf, indexes[i].MaxGlobalRawDataSizeMB)
		}

		// Add a new line in betwen index stanzas
		// Do not add config beyond here
		indexesConf = fmt.Sprintf(`%s
`, indexesConf)
	}

	return indexesConf
}

//GetServerConfigEntries prepares the server.conf entries, and returns as a string
func GetServerConfigEntries(cacheManagerConf *enterpriseApi.CacheManagerSpec) string {
	if cacheManagerConf == nil {
		return ""
	}

	var serverConfIni string
	serverConfIni = fmt.Sprintf(`[cachemanager]`)

	emptyStanza := serverConfIni

	if cacheManagerConf.EvictionPaddingSizeMB != 0 {
		serverConfIni = fmt.Sprintf(`%s
eviction_padding = %d`, serverConfIni, cacheManagerConf.EvictionPaddingSizeMB)
	}

	if cacheManagerConf.EvictionPolicy != "" {
		serverConfIni = fmt.Sprintf(`%s
eviction_policy = %s`, serverConfIni, cacheManagerConf.EvictionPolicy)
	}

	if cacheManagerConf.HotlistBloomFilterRecencyHours != 0 {
		serverConfIni = fmt.Sprintf(`%s
hotlist_bloom_filter_recency_hours = %d`, serverConfIni, cacheManagerConf.HotlistBloomFilterRecencyHours)
	}

	if cacheManagerConf.HotlistRecencySecs != 0 {
		serverConfIni = fmt.Sprintf(`%s
hotlist_recency_secs = %d`, serverConfIni, cacheManagerConf.HotlistRecencySecs)
	}

	if cacheManagerConf.MaxCacheSizeMB != 0 {
		serverConfIni = fmt.Sprintf(`%s
max_cache_size = %d`, serverConfIni, cacheManagerConf.MaxCacheSizeMB)
	}

	if cacheManagerConf.MaxConcurrentDownloads != 0 {
		serverConfIni = fmt.Sprintf(`%s
max_concurrent_downloads = %d`, serverConfIni, cacheManagerConf.MaxConcurrentDownloads)
	}

	if cacheManagerConf.MaxConcurrentUploads != 0 {
		serverConfIni = fmt.Sprintf(`%s
max_concurrent_uploads = %d`, serverConfIni, cacheManagerConf.MaxConcurrentUploads)
	}

	if emptyStanza == serverConfIni {
		return ""
	}

	serverConfIni = fmt.Sprintf(`%s
`, serverConfIni)

	return serverConfIni
}

// GetSmartstoreIndexesDefaults fills the indexes.conf default stanza in INI format
func GetSmartstoreIndexesDefaults(defaults enterpriseApi.IndexConfDefaultsSpec) string {

	remotePath := "$_index_name"

	indexDefaults := fmt.Sprintf(`[default]
repFactor = auto
maxDataSize = auto
homePath = $SPLUNK_DB/%s/db
coldPath = $SPLUNK_DB/%s/colddb
thawedPath = $SPLUNK_DB/%s/thaweddb`,
		remotePath, remotePath, remotePath)

	// Do not change any of the following Sprintf formats(Intentionally indented)
	if defaults.VolName != "" {
		//if defaults.VolName != "" && defaults.RemotePath != "" {
		indexDefaults = fmt.Sprintf(`%s
remotePath = volume:%s/%s`, indexDefaults, defaults.VolName, remotePath)
	}

	if defaults.MaxGlobalDataSizeMB != 0 {
		indexDefaults = fmt.Sprintf(`%s
maxGlobalDataSizeMB = %d`, indexDefaults, defaults.MaxGlobalDataSizeMB)
	}

	if defaults.MaxGlobalRawDataSizeMB != 0 {
		indexDefaults = fmt.Sprintf(`%s
maxGlobalRawDataSizeMB = %d`, indexDefaults, defaults.MaxGlobalRawDataSizeMB)
	}

	indexDefaults = fmt.Sprintf(`%s
`, indexDefaults)
	return indexDefaults
}
