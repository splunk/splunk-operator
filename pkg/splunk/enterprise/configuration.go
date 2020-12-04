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

package enterprise

import (
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var logC = logf.Log.WithName("splunk.enterprise.configValidation")

// getSplunkLabels returns a map of labels to use for Splunk Enterprise components.
func getSplunkLabels(instanceIdentifier string, instanceType InstanceType, partOfIdentifier string) map[string]string {
	// For multisite / multipart IndexerCluster, the name of the part containing the cluster-master is used
	// to set the label app.kubernetes.io/part-of on all the parts so that its indexer service can select
	// the indexers from all the parts. Otherwise partOfIdentifier is equal to instanceIdentifier.
	if instanceType != SplunkIndexer || len(partOfIdentifier) == 0 {
		partOfIdentifier = instanceIdentifier
	}
	return splcommon.GetLabels(instanceType.ToKind(), instanceType.ToString(), instanceIdentifier, partOfIdentifier, make([]string, 0))
}

// getSplunkVolumeClaims returns a standard collection of Kubernetes volume claims.
func getSplunkVolumeClaims(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec, labels map[string]string) ([]corev1.PersistentVolumeClaim, error) {
	var etcStorage, varStorage resource.Quantity
	var err error

	etcStorage, err = splcommon.ParseResourceQuantity(spec.EtcStorage, "10Gi")
	if err != nil {
		return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "etcStorage", err)
	}

	varStorage, err = splcommon.ParseResourceQuantity(spec.VarStorage, "100Gi")
	if err != nil {
		return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "varStorage", err)
	}

	volumeClaims := []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-etc",
				Namespace: cr.GetNamespace(),
				Labels:    labels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: etcStorage,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-var",
				Namespace: cr.GetNamespace(),
				Labels:    labels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: varStorage,
					},
				},
			},
		},
	}

	if spec.StorageClassName != "" {
		for idx := range volumeClaims {
			volumeClaims[idx].Spec.StorageClassName = &spec.StorageClassName
		}
	}

	return volumeClaims, nil
}

// getSplunkService returns a Kubernetes Service object for Splunk instances configured for a Splunk Enterprise resource.
func getSplunkService(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, isHeadless bool) *corev1.Service {

	// use template if not headless
	var service *corev1.Service
	if isHeadless {
		service = &corev1.Service{}

		// Initialize to defaults
		service.Spec.ClusterIP = corev1.ClusterIPNone
		service.Spec.Type = corev1.ServiceTypeClusterIP
	} else {
		service = spec.Spec.ServiceTemplate.DeepCopy()
	}
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	service.ObjectMeta.Name = GetSplunkServiceName(instanceType, cr.GetName(), isHeadless)
	service.ObjectMeta.Namespace = cr.GetNamespace()
	instanceIdentifier := cr.GetName()
	var partOfIdentifier string
	if instanceType == SplunkMonitoringConsole {
		service.ObjectMeta.Name = GetSplunkServiceName(instanceType, cr.GetNamespace(), isHeadless)
		instanceIdentifier = cr.GetNamespace()
	}
	if instanceType == SplunkIndexer {
		if len(spec.ClusterMasterRef.Name) == 0 {
			// Do not specify the instance label in the selector of IndexerCluster services, so that the services of the main part
			// of multisite / multipart IndexerCluster can be used to resolve (headless) or load balance traffic to the indexers of all parts
			partOfIdentifier = instanceIdentifier
			instanceIdentifier = ""
		} else {
			// And for child parts of multisite / multipart IndexerCluster, use the name of the part containing the cluster-master
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
func setVolumeDefaults(spec *enterprisev1.CommonSplunkSpec) {

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

// validateCommonSplunkSpec checks validity and makes default updates to a CommonSplunkSpec, and returns error if something is wrong.
func validateCommonSplunkSpec(spec *enterprisev1.CommonSplunkSpec) error {
	// if not specified via spec or env, image defaults to splunk/splunk
	spec.Spec.Image = GetSplunkImage(spec.Spec.Image)

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

	setVolumeDefaults(spec)

	return splcommon.ValidateSpec(&spec.Spec, defaultResources)
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

// prepareSplunkSmartstoreConfigMap returns a K8 ConfigMap containing Splunk smartstore config in INI format
func prepareSplunkSmartstoreConfigMap(identifier, namespace string, crKind string, dataIniMap map[string]string) *corev1.ConfigMap {
	configMapIni := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkSmartstoreConfigMapName(identifier, crKind),
			Namespace: namespace,
		},
	}
	configMapIni.Data = dataIniMap

	return configMapIni
}

// getSplunkPorts returns a map of ports to use for Splunk instances.
func getSplunkPorts(instanceType InstanceType) map[string]int {
	result := map[string]int{
		"splunkweb": 8000,
		"splunkd":   8089,
	}

	switch instanceType {
	case SplunkMonitoringConsole:
		result["hec"] = 8088
		result["s2s"] = 9997
	case SplunkStandalone:
		result["dfccontrol"] = 17000
		result["datareceive"] = 19000
		result["dfsmaster"] = 9000
		result["hec"] = 8088
		result["s2s"] = 9997
	case SplunkSearchHead:
		result["dfccontrol"] = 17000
		result["datareceive"] = 19000
		result["dfsmaster"] = 9000
	case SplunkIndexer:
		result["hec"] = 8088
		result["s2s"] = 9997
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

// getSplunkStatefulSet returns a Kubernetes StatefulSet object for Splunk instances configured for a Splunk Enterprise resource.
func getSplunkStatefulSet(client splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, replicas int32, extraEnv []corev1.EnvVar) (*appsv1.StatefulSet, error) {

	// prepare misc values
	ports := splcommon.SortContainerPorts(getSplunkContainerPorts(instanceType)) // note that port order is important for tests
	annotations := splcommon.GetIstioAnnotations(ports)
	selectLabels := getSplunkLabels(cr.GetName(), instanceType, spec.ClusterMasterRef.Name)
	affinity := splcommon.AppendPodAntiAffinity(&spec.Affinity, cr.GetName(), instanceType.ToString())

	// start with same labels as selector; note that this object gets modified by splcommon.AppendParentMeta()
	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	// create statefulset configuration
	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(instanceType, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
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
					Affinity:      affinity,
					Tolerations:   spec.Tolerations,
					SchedulerName: spec.SchedulerName,
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
		},
	}

	// update template to include storage for etc and var volumes
	if spec.EphemeralStorage {
		// add ephemeral volumes to the splunk pod for etc and opt
		emptyVolumeSource := corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
		statefulSet.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: "mnt-splunk-etc", VolumeSource: emptyVolumeSource},
			{Name: "mnt-splunk-var", VolumeSource: emptyVolumeSource},
		}

		// add volume mounts to splunk container for the ephemeral volumes
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "mnt-splunk-etc",
				MountPath: "/opt/splunk/etc",
			},
			{
				Name:      "mnt-splunk-var",
				MountPath: "/opt/splunk/var",
			},
		}

	} else {
		// prepare and append persistent volume claims if storage is not ephemeral
		var err error
		statefulSet.Spec.VolumeClaimTemplates, err = getSplunkVolumeClaims(cr, spec, labels)
		if err != nil {
			return nil, err
		}

		// add volume mounts to splunk container for the PVCs
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "pvc-etc",
				MountPath: "/opt/splunk/etc",
			},
			{
				Name:      "pvc-var",
				MountPath: "/opt/splunk/var",
			},
		}
	}

	// append labels and annotations from parent
	splcommon.AppendParentMeta(statefulSet.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())

	// retrieve the secret to upload to the statefulSet pod
	statefulSetSecret, err := splutil.GetLatestVersionedSecret(client, cr, cr.GetNamespace(), statefulSet.GetName())
	if err != nil || statefulSetSecret == nil {
		return statefulSet, err
	}

	// update statefulset's pod template with common splunk pod config
	updateSplunkPodTemplateWithConfig(client, &statefulSet.Spec.Template, cr, spec, instanceType, extraEnv, statefulSetSecret.GetName())

	// make Splunk Enterprise object the owner
	statefulSet.SetOwnerReferences(append(statefulSet.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	return statefulSet, nil
}

// getSmartstoreConfigMap returns the smartstore configMap, if it exists and applicable for that instanceType
func getSmartstoreConfigMap(client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType) (*corev1.ConfigMap, bool) {
	var smartStoreConfigMapName string
	if instanceType == SplunkStandalone || instanceType == SplunkClusterMaster {
		smartStoreConfigMapName = GetSplunkSmartstoreConfigMapName(cr.GetName(), cr.GetObjectKind().GroupVersionKind().Kind)
	}

	if smartStoreConfigMapName != "" {
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: smartStoreConfigMapName}
		configMap, err := splctrl.GetConfigMap(client, namespacedName)
		if err != nil {
			// Do not return configMap name, unless the configMap really exists
			return nil, false
		}

		return configMap, true
	}

	// Do not return configMap name, unless the configMap really exists
	return nil, false
}

// updateSplunkPodTemplateWithConfig modifies the podTemplateSpec object based on configuration of the Splunk Enterprise resource.
func updateSplunkPodTemplateWithConfig(client splcommon.ControllerClient, podTemplateSpec *corev1.PodTemplateSpec, cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, extraEnv []corev1.EnvVar, secretToMount string) {

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
	if spec.Volumes != nil && instanceType != SplunkMonitoringConsole {
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
	if spec.Defaults != "" && instanceType != SplunkMonitoringConsole {
		configMapName := GetSplunkDefaultsName(cr.GetName(), instanceType)
		addSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-defaults", "/mnt/splunk-defaults", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
				DefaultMode: &configMapVolDefaultMode,
			},
		})

		scopedLog := log.WithName("updateSplunkPodTemplateWithConfig").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

		// We will update the annotation for resource version in the pod template spec
		// so that any change in the ConfigMap will lead to recycle of the pod.
		configMapResourceVersion, err := splctrl.GetConfigMapResourceVersion(client, namespacedName)
		if err == nil {
			podTemplateSpec.ObjectMeta.Annotations["defaultConfigRev"] = configMapResourceVersion
		} else {
			scopedLog.Error(err, "Updation of default configMap annotation failed")
		}
	}

	smartstoreConfigMap, exists := getSmartstoreConfigMap(client, cr, instanceType)
	if exists {
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
		// propagated through the CM master apps bundle push
		// 2. In case of Standalone, reset the Pod, by updating the latest Resource version of the
		// smartstore config map.
		if instanceType == SplunkStandalone {
			podTemplateSpec.ObjectMeta.Annotations[smartStoreConfigRev] = smartstoreConfigMap.ResourceVersion
		}

	}

	// update security context
	runAsUser := int64(41812)
	fsGroup := int64(41812)
	podTemplateSpec.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser: &runAsUser,
		FSGroup:   &fsGroup,
	}

	// use script provided by enterprise container to check if pod is alive
	livenessProbe := &corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/sbin/checkstate.sh",
				},
			},
		},
		InitialDelaySeconds: 300,
		TimeoutSeconds:      30,
		PeriodSeconds:       30,
	}

	// pod is ready if container artifact file is created with contents of "started".
	// this indicates that all the the ansible plays executed at startup have completed.
	readinessProbe := &corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/grep",
					"started",
					"/opt/container_artifact/splunk-container.state",
				},
			},
		},
		InitialDelaySeconds: 10,
		TimeoutSeconds:      5,
		PeriodSeconds:       5,
	}

	// prepare defaults variable
	splunkDefaults := "/mnt/splunk-secrets/default.yml"
	if spec.DefaultsURL != "" {
		splunkDefaults = fmt.Sprintf("%s,%s", spec.DefaultsURL, splunkDefaults)
	}
	if spec.Defaults != "" {
		splunkDefaults = fmt.Sprintf("%s,%s", "/mnt/splunk-defaults/default.yml", splunkDefaults)
	}

	// prepare container env variables
	role := instanceType.ToRole()
	if instanceType == SplunkStandalone && len(spec.ClusterMasterRef.Name) > 0 {
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
	if os.Getenv("SPLUNK_HTTP_ENABLESSL") == "true" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_HTTP_ENABLESSL",
			Value: "true",
		})
	}

	if os.Getenv("SPLUNKD_SSL_ENABLE") == "false" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_CERT_PREFIX",
			Value: "http",
		})
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNKD_SSL_ENABLE",
			Value: "false",
		})
		env = append(env, corev1.EnvVar{
			Name:  "NO_HEALTHCHECK",
			Value: "true",
		})
	}
	if os.Getenv("SPLUNKD_SSL_ENABLE") == "false" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNKD_SSL_ENABLE",
			Value: "false",
		})
	}
	// update variables for licensing, if configured
	if spec.LicenseURL != "" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_LICENSE_URI",
			Value: spec.LicenseURL,
		})
	}
	if instanceType != SplunkLicenseMaster && spec.LicenseMasterRef.Name != "" {
		licenseMasterName := GetSplunkServiceName(SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
		var namespace string
		if spec.LicenseMasterRef.Namespace == "" {
			namespace = cr.GetNamespace()
		} else {
			namespace = spec.LicenseMasterRef.Namespace
		}
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: splcommon.GetServiceURI(namespace, licenseMasterName),
		})
	}

	// append URL for cluster master, if configured
	var clusterMasterURL string
	if instanceType == SplunkClusterMaster {
		// This makes splunk-ansible configure indexer-discovery on cluster-master
		clusterMasterURL = "localhost"
	} else if spec.ClusterMasterRef.Name != "" {
		clusterMasterURL = GetSplunkServiceName(SplunkClusterMaster, spec.ClusterMasterRef.Name, false)
		if spec.ClusterMasterRef.Namespace != "" {
			clusterMasterURL = splcommon.GetServiceURI(spec.ClusterMasterRef.Namespace, clusterMasterURL)
		}
	}
	if clusterMasterURL != "" {
		extraEnv = append(extraEnv, corev1.EnvVar{
			Name:  "SPLUNK_CLUSTER_MASTER_URL",
			Value: clusterMasterURL,
		})
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

// isSmartstoreEnabled checks and returns true if smartstore is configured
func isSmartstoreConfigured(smartstore *enterprisev1.SmartStoreSpec) bool {
	if smartstore == nil {
		return false
	}

	return smartstore.IndexList != nil || smartstore.VolList != nil || smartstore.Defaults.VolName != ""
}

func checkIfVolumeExists(volumeList []enterprisev1.VolumeSpec, volName string) (int, error) {
	for i, volume := range volumeList {
		if volume.Name == volName {
			return i, nil
		}
	}

	return -1, fmt.Errorf("Volume: %s, doesn't exist", volName)
}

// AreRemoteVolumeKeysChanged discovers if the S3 keys changed
func AreRemoteVolumeKeysChanged(client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType, smartstore *enterprisev1.SmartStoreSpec, ResourceRev map[string]string, retError *error) bool {
	// No need to proceed if the smartstore is not configured
	if isSmartstoreConfigured(smartstore) == false {
		return false
	}

	scopedLog := log.WithName("CheckIfsmartstoreConfigMapUpdatedToPod").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	volList := smartstore.VolList
	for _, volume := range volList {
		namespaceScopedSecret, err := splutil.GetSecretByName(client, cr, volume.SecretRef)
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
	}

	return false
}

// ValidateSplunkSmartstoreSpec checks and validates the smartstore config
func ValidateSplunkSmartstoreSpec(smartstore *enterprisev1.SmartStoreSpec) error {
	var err error

	// Smartstore is an optional config (at least) for now
	if !isSmartstoreConfigured(smartstore) {
		return nil
	}

	numVolumes := len(smartstore.VolList)
	numIndexes := len(smartstore.IndexList)
	if numIndexes > 0 && numVolumes == 0 {
		return fmt.Errorf("Volume configuration is missing. Num. of indexes = %d. Num. of Volumes = %d", numIndexes, numVolumes)
	}

	duplicateChecker := make(map[string]bool)

	volList := smartstore.VolList
	// Make sure that all the Volumes are provided with the mandatory config values.
	for i, volume := range volList {
		if _, ok := duplicateChecker[volume.Name]; ok {
			return fmt.Errorf("Duplicate volume name detected: %s. Remove the duplicate entry and reapply the configuration", volume.Name)
		}
		duplicateChecker[volume.Name] = true

		// Make sure that the smartstore volume info is correct
		if volume.Name == "" {
			return fmt.Errorf("Volume name is missing for volume at : %d", i)
		}

		if volume.Endpoint == "" {
			return fmt.Errorf("Volume Endpoint URI is missing")
		}

		if volume.Path == "" {
			return fmt.Errorf("Volume Path is missing")
		}

		if volume.SecretRef == "" {
			return fmt.Errorf("Volume SecretRef is missing")
		}
	}

	defaults := smartstore.Defaults
	// When volName is configured, bucket remote path should also be configured
	if defaults.VolName != "" {
		_, err = checkIfVolumeExists(volList, defaults.VolName)
		if err != nil {
			return fmt.Errorf("Invalid configuration for defaults volume. %s", err)
		}
	}

	duplicateChecker = make(map[string]bool)
	indexList := smartstore.IndexList
	// Make sure that all the indexes are provided with the mandatory config values.
	for i, index := range indexList {
		if _, ok := duplicateChecker[index.Name]; ok {
			return fmt.Errorf("Duplicate index name detected: %s.Remove the duplicate entry and reapply the configuration", index.Name)
		}
		duplicateChecker[index.Name] = true

		if index.Name == "" {
			return fmt.Errorf("Index name is missing for index at: %d", i)
		}

		if index.VolName == "" && defaults.VolName == "" {
			return fmt.Errorf("volumeName is missing for index: %s", index.Name)
		}

		if index.VolName != "" {
			_, err = checkIfVolumeExists(volList, index.VolName)
			if err != nil {
				return fmt.Errorf("Invalid configuration for index: %s. %s", index.Name, err)
			}
		}

	}

	return nil
}

// GetSmartstoreVolumesConfig returns the list of Volumes configuration in INI format
func GetSmartstoreVolumesConfig(client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterprisev1.SmartStoreSpec, mapData map[string]string) (string, error) {
	var volumesConf string

	volumes := smartstore.VolList
	for i := 0; i < len(volumes); i++ {
		s3AccessKey, s3SecretKey, _, err := GetSmartstoreRemoteVolumeSecrets(volumes[i], client, cr, smartstore)
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
	}

	return volumesConf, nil
}

// GetSmartstoreIndexesConfig returns the list of indexes configuration in INI format
func GetSmartstoreIndexesConfig(indexes []enterprisev1.IndexSpec) string {

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
func GetServerConfigEntries(cacheManagerConf *enterprisev1.CacheManagerSpec) string {
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
func GetSmartstoreIndexesDefaults(defaults enterprisev1.IndexConfDefaultsSpec) string {

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
