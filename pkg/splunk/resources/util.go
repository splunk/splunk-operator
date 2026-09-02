// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package resources

import (
	"context"
	"fmt"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func prepareConfigMap(name, namespace string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Data: data}
}

// SetupInitContainer adds the SmartStore symlink initializer to a pod template.
func SetupInitContainer(podTemplateSpec *corev1.PodTemplateSpec, image, imagePullPolicy, command string, etcEphemeral bool) {
	volumeName := fmt.Sprintf(splcommon.SplunkMountNamePrefix, splcommon.EtcVolumeStorage)
	if !etcEphemeral {
		volumeName = fmt.Sprintf(splcommon.PvcNamePrefix, splcommon.EtcVolumeStorage)
	}
	runAsUser := int64(41812)
	runAsNonRoot := true
	privileged := false
	podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, corev1.Container{
		Image: image, ImagePullPolicy: corev1.PullPolicy(imagePullPolicy), Name: "init",
		Command:         []string{"bash", "-c", command},
		VolumeMounts:    []corev1.VolumeMount{{Name: volumeName, MountPath: "/opt/splk/etc"}},
		Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("0.25"), corev1.ResourceMemory: resource.MustParse("128Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")}},
		SecurityContext: &corev1.SecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, AllowPrivilegeEscalation: boolPtr(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"NET_BIND_SERVICE"}}, Privileged: &privileged, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
	})
}

func boolPtr(value bool) *bool { return &value }

var defaultLivenessProbe = corev1.Probe{
	InitialDelaySeconds: 30,
	TimeoutSeconds:      30,
	PeriodSeconds:       30,
	FailureThreshold:    3,
	ProbeHandler: corev1.ProbeHandler{
		Exec: &corev1.ExecAction{
			Command: []string{splutil.GetProbeMountDirectory() + "/" + splutil.GetLivenessScriptName()},
		},
	},
}

var defaultReadinessProbe = corev1.Probe{
	InitialDelaySeconds: 10,
	TimeoutSeconds:      5,
	PeriodSeconds:       5,
	FailureThreshold:    3,
	ProbeHandler: corev1.ProbeHandler{
		Exec: &corev1.ExecAction{
			Command: []string{splutil.GetProbeMountDirectory() + "/" + splutil.GetReadinessScriptName()},
		},
	},
}

var defaultStartupProbe = corev1.Probe{
	InitialDelaySeconds: 40,
	TimeoutSeconds:      30,
	PeriodSeconds:       30,
	FailureThreshold:    12,
	ProbeHandler: corev1.ProbeHandler{
		Exec: &corev1.ExecAction{
			Command: []string{splutil.GetProbeMountDirectory() + "/" + splutil.GetStartupScriptName()},
		},
	},
}

// GetSplunkLabels returns labels for a Splunk Enterprise component.
func GetSplunkLabels(instanceIdentifier string, instanceType splcommon.InstanceType, partOfIdentifier string) map[string]string {
	if instanceType != splcommon.SplunkIndexer || len(partOfIdentifier) == 0 {
		partOfIdentifier = instanceIdentifier
	}
	labels, _ := splcommon.GetLabels(instanceType.ToKind(), instanceType.ToString(), instanceIdentifier, partOfIdentifier, make([]string, 0))
	return labels
}

// GetSplunkVolumeClaims returns a standard collection of Kubernetes volume claims.
func GetSplunkVolumeClaims(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, labels map[string]string, volumeType string, adminManagedPV bool) (corev1.PersistentVolumeClaim, error) {
	var storageCapacity resource.Quantity
	var err error
	var storageClassName string

	switch volumeType {
	case splcommon.EtcVolumeStorage:
		storageCapacity, err = splcommon.ParseResourceQuantity(spec.EtcVolumeStorageConfig.StorageCapacity, splcommon.DefaultEtcVolumeStorageCapacity)
		if err != nil {
			return corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "etcStorage", err)
		}
		storageClassName = spec.EtcVolumeStorageConfig.StorageClassName
	case splcommon.VarVolumeStorage:
		storageCapacity, err = splcommon.ParseResourceQuantity(spec.VarVolumeStorageConfig.StorageCapacity, splcommon.DefaultVarVolumeStorageCapacity)
		if err != nil {
			return corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "varStorage", err)
		}
		storageClassName = spec.VarVolumeStorageConfig.StorageClassName
	}

	volumeClaim := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(splcommon.PvcNamePrefix, volumeType),
			Namespace: cr.GetNamespace(),
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: storageCapacity},
			},
		},
	}
	if adminManagedPV {
		volumeClaim.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{
			"app.kubernetes.io/name":     labels["app.kubernetes.io/name"],
			"app.kubernetes.io/instance": labels["app.kubernetes.io/instance"],
		}}
	} else if storageClassName != "" {
		volumeClaim.Spec.StorageClassName = &storageClassName
	}
	return volumeClaim, nil
}

// GetSplunkService returns a Service object for a Splunk Enterprise resource.
func GetSplunkService(_ context.Context, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType splcommon.InstanceType, isHeadless bool) *corev1.Service {
	var service *corev1.Service
	if isHeadless {
		service = &corev1.Service{Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone, Type: corev1.ServiceTypeClusterIP}}
	} else {
		service = spec.ServiceTemplate.DeepCopy()
	}
	service.TypeMeta = metav1.TypeMeta{Kind: "Service", APIVersion: "v1"}
	service.ObjectMeta.Name = splcommon.GetSplunkServiceName(instanceType, cr.GetName(), isHeadless)
	service.ObjectMeta.Namespace = cr.GetNamespace()
	instanceIdentifier := cr.GetName()
	var partOfIdentifier string
	if instanceType == splcommon.SplunkIndexer {
		if len(spec.ClusterManagerRef.Name) == 0 && len(spec.ClusterMasterRef.Name) == 0 {
			partOfIdentifier = instanceIdentifier
			instanceIdentifier = ""
		} else if len(spec.ClusterManagerRef.Name) > 0 {
			partOfIdentifier = spec.ClusterManagerRef.Name
		} else if len(spec.ClusterMasterRef.Name) > 0 {
			partOfIdentifier = spec.ClusterMasterRef.Name
		}
	}
	service.Spec.Selector = GetSplunkLabels(instanceIdentifier, instanceType, partOfIdentifier)
	service.Spec.Ports = append(service.Spec.Ports, splcommon.SortServicePorts(GetSplunkServicePorts(instanceType))...)
	if service.ObjectMeta.Labels == nil {
		service.ObjectMeta.Labels = make(map[string]string)
	}
	if service.ObjectMeta.Annotations == nil {
		service.ObjectMeta.Annotations = make(map[string]string)
	}
	for k, v := range service.Spec.Selector {
		service.ObjectMeta.Labels[k] = v
	}
	splcommon.AppendParentMeta(service.ObjectMeta.GetObjectMeta(), cr.GetObjectMeta())
	if instanceType == splcommon.SplunkDeployer || (instanceType == splcommon.SplunkSearchHead && isHeadless) {
		service.Spec.PublishNotReadyAddresses = true
	}
	service.SetOwnerReferences(append(service.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
	return service
}

// SetVolumeDefaults sets default modes for Secret and ConfigMap volumes.
func SetVolumeDefaults(spec *enterpriseApi.CommonSplunkSpec) {
	if spec.Volumes == nil {
		spec.Volumes = []corev1.Volume{}
	}
	for _, v := range spec.Volumes {
		if v.Secret != nil {
			if v.Secret.DefaultMode == nil {
				perm := corev1.SecretVolumeSourceDefaultMode
				v.Secret.DefaultMode = &perm
			}
			continue
		}
		if v.ConfigMap != nil {
			if v.ConfigMap.DefaultMode == nil {
				perm := corev1.ConfigMapVolumeSourceDefaultMode
				v.ConfigMap.DefaultMode = &perm
			}
		}
	}
}

// GetSplunkDefaults returns the defaults ConfigMap for a Splunk resource.
func GetSplunkDefaults(identifier, namespace string, instanceType splcommon.InstanceType, defaults string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: splutil.GetSplunkDefaultsName(identifier, instanceType), Namespace: namespace},
		Data:       map[string]string{"default.yml": defaults},
	}
}

// GetSplunkPorts returns ports for a Splunk instance.
func GetSplunkPorts(instanceType splcommon.InstanceType) map[string]int {
	result := map[string]int{
		splutil.GetPortName("splunkweb", "http"): 8000,
		splutil.GetPortName("splunkd", "https"):  8089,
	}
	switch instanceType {
	case splcommon.SplunkMonitoringConsole, splcommon.SplunkStandalone, splcommon.SplunkIndexer, splcommon.SplunkIngestor:
		result[splutil.GetPortName("hec", "http")] = 8088
		result[splutil.GetPortName("s2s", "tcp")] = 9997
	}
	return result
}

// GetSplunkContainerPorts returns container ports for a Splunk instance.
func GetSplunkContainerPorts(instanceType splcommon.InstanceType) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{}
	for key, value := range GetSplunkPorts(instanceType) {
		ports = append(ports, corev1.ContainerPort{Name: key, ContainerPort: int32(value), Protocol: corev1.ProtocolTCP})
	}
	return ports
}

// GetSplunkServicePorts returns Service ports for a Splunk instance.
func GetSplunkServicePorts(instanceType splcommon.InstanceType) []corev1.ServicePort {
	ports := []corev1.ServicePort{}
	for key, value := range GetSplunkPorts(instanceType) {
		ports = append(ports, corev1.ServicePort{Name: key, Port: int32(value), TargetPort: intstr.FromInt(value), Protocol: corev1.ProtocolTCP})
	}
	return ports
}

// AddSplunkVolumeToTemplate adds a volume and mount to a pod template.
func AddSplunkVolumeToTemplate(podTemplateSpec *corev1.PodTemplateSpec, name, mountPath string, volumeSource corev1.VolumeSource) {
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, corev1.Volume{Name: name, VolumeSource: volumeSource})
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(podTemplateSpec.Spec.Containers[idx].VolumeMounts, corev1.VolumeMount{Name: name, MountPath: mountPath})
	}
}

// AddPVCVolumes adds a PVC and its mount to a StatefulSet.
func AddPVCVolumes(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, statefulSet *appsv1.StatefulSet, labels map[string]string, volumeType string) error {
	adminManagedPV := false
	if value, ok := cr.GetAnnotations()["enterprise.splunk.com/admin-managed-pv"]; ok && strings.ToLower(value) == "true" {
		adminManagedPV = true
	}
	volumeClaimTemplate, err := GetSplunkVolumeClaims(cr, spec, labels, volumeType, adminManagedPV)
	if err != nil {
		return err
	}
	statefulSet.Spec.VolumeClaimTemplates = append(statefulSet.Spec.VolumeClaimTemplates, volumeClaimTemplate)
	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name: volumeClaimTemplate.GetName(), MountPath: fmt.Sprintf(splcommon.SplunkMountDirecPrefix, volumeType),
	})
	return nil
}

// AddEphemeralVolumes adds an ephemeral volume and its mount to a StatefulSet.
func AddEphemeralVolumes(statefulSet *appsv1.StatefulSet, volumeType string) error {
	statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: fmt.Sprintf(splcommon.SplunkMountNamePrefix, volumeType), VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name: fmt.Sprintf(splcommon.SplunkMountNamePrefix, volumeType), MountPath: fmt.Sprintf(splcommon.SplunkMountDirecPrefix, volumeType),
	})
	return nil
}

// AddProbeConfigMapVolume mounts the probe ConfigMap in a StatefulSet.
func AddProbeConfigMapVolume(configMap *corev1.ConfigMap, statefulSet *appsv1.StatefulSet) {
	mode := splutil.GetProbeVolumePermission()
	AddSplunkVolumeToTemplate(&statefulSet.Spec.Template, configMap.Name, splutil.GetProbeMountDirectory(), corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configMap.GetName()}, DefaultMode: &mode},
	})
}

// InjectSplunkProvision adds the splunk-provision initializer and mounts.
func InjectSplunkProvision(splunkProvisionImage string, podTemplateSpec *corev1.PodTemplateSpec, extraEnv *[]corev1.EnvVar) {
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, corev1.Volume{Name: "splunk-provision-bin", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, corev1.Container{
		Name: "splunk-provision-init", Image: splunkProvisionImage, ImagePullPolicy: corev1.PullAlways,
		Command:      []string{"bash", "-c", "cp /opt/splunk-provision/splunk-provision /mnt/splunk-provision/splunk-provision && " + "cp /opt/splunk-provision/entrypoint.sh /mnt/splunk-provision/entrypoint.sh && " + "chmod 755 /mnt/splunk-provision/splunk-provision /mnt/splunk-provision/entrypoint.sh"},
		VolumeMounts: []corev1.VolumeMount{{Name: "splunk-provision-bin", MountPath: "/mnt/splunk-provision"}},
	})
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(podTemplateSpec.Spec.Containers[idx].VolumeMounts,
			corev1.VolumeMount{Name: "splunk-provision-bin", MountPath: "/sbin/entrypoint.sh", SubPath: "entrypoint.sh"},
			corev1.VolumeMount{Name: "splunk-provision-bin", MountPath: "/opt/splunk/bin/splunk-provision", SubPath: "splunk-provision"})
	}
	*extraEnv = append([]corev1.EnvVar{{Name: "SPLUNK_NO_ANSIBLE", Value: "true"}}, *extraEnv...)
}

// RemoveDuplicateEnvVars keeps the first occurrence of each environment variable.
func RemoveDuplicateEnvVars(sliceList []corev1.EnvVar) []corev1.EnvVar {
	allKeys := orderedmap.New[string, bool]()
	list := []corev1.EnvVar{}
	for _, item := range sliceList {
		if _, ok := allKeys.Get(item.Name); !ok {
			allKeys.Set(item.Name, true)
			list = append(list, item)
		}
	}
	return list
}

// GetLivenessProbe returns the configured or default liveness probe.
func GetLivenessProbe(configuredProbe *enterpriseApi.Probe, configuredDelay int32) *corev1.Probe {
	return GetProbeWithConfigUpdates(&defaultLivenessProbe, configuredProbe, configuredDelay)
}

// GetReadinessProbe returns the configured or default readiness probe.
func GetReadinessProbe(configuredProbe *enterpriseApi.Probe, configuredDelay int32) *corev1.Probe {
	return GetProbeWithConfigUpdates(&defaultReadinessProbe, configuredProbe, configuredDelay)
}

// GetStartupProbe returns the configured or default startup probe.
func GetStartupProbe(configuredProbe *enterpriseApi.Probe) *corev1.Probe {
	return GetProbeWithConfigUpdates(&defaultStartupProbe, configuredProbe, 0)
}

// GetProbeWithConfigUpdates applies configured values to a default probe.
func GetProbeWithConfigUpdates(defaultProbe *corev1.Probe, configuredProbe *enterpriseApi.Probe, configuredDelay int32) *corev1.Probe {
	if configuredProbe != nil {
		derivedProbe := corev1.Probe{InitialDelaySeconds: configuredProbe.InitialDelaySeconds, TimeoutSeconds: configuredProbe.TimeoutSeconds, PeriodSeconds: configuredProbe.PeriodSeconds, FailureThreshold: configuredProbe.FailureThreshold}
		if derivedProbe.InitialDelaySeconds == 0 {
			if configuredDelay != 0 {
				derivedProbe.InitialDelaySeconds = configuredDelay
			} else {
				derivedProbe.InitialDelaySeconds = defaultProbe.InitialDelaySeconds
			}
		}
		if derivedProbe.TimeoutSeconds == 0 {
			derivedProbe.TimeoutSeconds = defaultProbe.TimeoutSeconds
		}
		if derivedProbe.PeriodSeconds == 0 {
			derivedProbe.PeriodSeconds = defaultProbe.PeriodSeconds
		}
		if derivedProbe.FailureThreshold == 0 {
			derivedProbe.FailureThreshold = defaultProbe.FailureThreshold
		}
		derivedProbe.Exec = defaultProbe.Exec
		return &derivedProbe
	}
	if configuredDelay != 0 {
		derivedProbe := *defaultProbe
		derivedProbe.InitialDelaySeconds = configuredDelay
		return &derivedProbe
	}
	return defaultProbe
}

// GetProbe returns a probe with the supplied command and timings.
func GetProbe(command []string, delay, timeout, period int32) *corev1.Probe {
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: command}}, InitialDelaySeconds: delay, TimeoutSeconds: timeout, PeriodSeconds: period}
}

// GetVolumeSourceMountFromConfigMapData returns a ConfigMap volume with all data entries mounted.
func GetVolumeSourceMountFromConfigMapData(configMap *corev1.ConfigMap, mode *int32) corev1.VolumeSource {
	volumeSource := corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configMap.GetName()}, DefaultMode: mode}}
	for key := range configMap.Data {
		volumeSource.ConfigMap.Items = append(volumeSource.ConfigMap.Items, corev1.KeyToPath{Key: key, Path: key, Mode: mode})
	}
	splcommon.SortSlice(volumeSource.ConfigMap.Items, splcommon.SortFieldKey)
	return volumeSource
}

// TODO(SPL-307034): Move this check to `splunk-provision` - it should know which roles it support.
// This does not account for unsupported common features - like IPv6, multisite etc.
// SplunkProvisionSupportsRole reports whether splunk-provision supports a role.
func SplunkProvisionSupportsRole(instanceType splcommon.InstanceType) bool {
	return instanceType == splcommon.SplunkSearchHead || instanceType == splcommon.SplunkDeployer
}
