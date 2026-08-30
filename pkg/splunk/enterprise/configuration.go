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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

const (
	splunkKVStoreDefaultTypeEnv = "SPLUNK_KVSTORE_DEFAULT_TYPE"
	splunkKVStoreTypeLocal      = "local"
)

var defaultLivenessProbe corev1.Probe = corev1.Probe{
	InitialDelaySeconds: livenessProbeDefaultDelaySec,
	TimeoutSeconds:      livenessProbeTimeoutSec,
	PeriodSeconds:       livenessProbePeriodSec,
	FailureThreshold:    livenessProbeFailureThreshold,
	ProbeHandler: corev1.ProbeHandler{
		Exec: &corev1.ExecAction{
			Command: []string{
				GetProbeMountDirectory() + "/" + GetLivenessScriptName(),
			},
		},
	},
}

var defaultReadinessProbe corev1.Probe = corev1.Probe{
	InitialDelaySeconds: readinessProbeDefaultDelaySec,
	TimeoutSeconds:      readinessProbeTimeoutSec,
	PeriodSeconds:       readinessProbePeriodSec,
	FailureThreshold:    readinessProbeFailureThreshold,
	ProbeHandler: corev1.ProbeHandler{
		Exec: &corev1.ExecAction{
			Command: []string{
				GetProbeMountDirectory() + "/" + GetReadinessScriptName(),
			},
		},
	},
}

var defaultStartupProbe corev1.Probe = corev1.Probe{
	InitialDelaySeconds: startupProbeDefaultDelaySec,
	TimeoutSeconds:      startupProbeTimeoutSec,
	PeriodSeconds:       startupProbePeriodSec,
	FailureThreshold:    startupProbeFailureThreshold,
	ProbeHandler: corev1.ProbeHandler{
		Exec: &corev1.ExecAction{
			Command: []string{
				GetProbeMountDirectory() + "/" + GetStartupScriptName(),
			},
		},
	},
}

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
func getSplunkVolumeClaims(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, labels map[string]string, volumeType string, adminManagedPV bool) (corev1.PersistentVolumeClaim, error) {
	var storageCapacity resource.Quantity
	var err error
	var storageClassName string
	var volumeClaim corev1.PersistentVolumeClaim

	// Depending on the volume type, determine storage capacity and storage class name (if configured)
	switch volumeType {
	case splcommon.EtcVolumeStorage:
		storageCapacity, err = splcommon.ParseResourceQuantity(
			spec.EtcVolumeStorageConfig.StorageCapacity,
			splcommon.DefaultEtcVolumeStorageCapacity,
		)
		if err != nil {
			return corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "etcStorage", err)
		}
		storageClassName = spec.EtcVolumeStorageConfig.StorageClassName

	case splcommon.VarVolumeStorage:
		storageCapacity, err = splcommon.ParseResourceQuantity(
			spec.VarVolumeStorageConfig.StorageCapacity,
			splcommon.DefaultVarVolumeStorageCapacity,
		)
		if err != nil {
			return corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "varStorage", err)
		}
		storageClassName = spec.VarVolumeStorageConfig.StorageClassName
	}

	if adminManagedPV {
		volumeClaim.Spec.StorageClassName = nil

		volumeClaim = corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(splcommon.PvcNamePrefix, volumeType),
				Namespace: cr.GetNamespace(),
				Labels:    labels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: storageCapacity,
					},
				},
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name":     labels["app.kubernetes.io/name"],
						"app.kubernetes.io/instance": labels["app.kubernetes.io/instance"],
					},
				},
			},
		}
	} else {
		volumeClaim = corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(splcommon.PvcNamePrefix, volumeType),
				Namespace: cr.GetNamespace(),
				Labels:    labels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: storageCapacity,
					},
				},
			},
		}

		if storageClassName != "" {
			volumeClaim.Spec.StorageClassName = &storageClassName
		}

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

	service.ObjectMeta.Name = splcommon.GetSplunkServiceName(instanceType, cr.GetName(), isHeadless)
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
	case "Always":
		break
	case "IfNotPresent":
		break
	default:
		return fmt.Errorf("ImagePullPolicy must be one of \"Always\" or \"IfNotPresent\"; value=\"%s\"", *imagePullPolicy)
	}
	return nil
}

// ValidateSpec checks validity and makes default updates to a Spec, and returns error if something is wrong.
func ValidateSpec(spec *enterpriseApi.Spec, defaultResources corev1.ResourceRequirements) error {
	// make sure SchedulerName is not empty
	if spec.SchedulerName == "" {
		spec.SchedulerName = "default-scheduler"
	}

	// set default values for service template
	setServiceTemplateDefaults(spec)

	spec.Resources = splutil.EffectiveResources(spec.Resources, spec.DisableResourceDefaults, defaultResources)

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
}

// validateCommonSplunkSpec checks validity and makes default updates to a CommonSplunkSpec, and returns error if something is wrong.
func validateCommonSplunkSpec(ctx context.Context, c splcommon.ControllerClient, spec *enterpriseApi.CommonSplunkSpec, cr splcommon.MetaObject) error {
	// if not specified via spec or env, image defaults to splunk/splunk
	spec.Image = GetSplunkImage(spec.Image)

	err := validateLivenessProbe(ctx, cr, spec.LivenessProbe)
	if err != nil {
		return err
	}

	err = validateReadinessProbe(ctx, cr, spec.ReadinessProbe)
	if err != nil {
		return err
	}

	err = validateStartupProbe(ctx, cr, spec.StartupProbe)
	if err != nil {
		return err
	}

	if spec.LivenessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Liveness probe initial delay", spec.LivenessInitialDelaySeconds)
	}

	if spec.ReadinessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Readiness probe initial delay", spec.ReadinessInitialDelaySeconds)
	}

	err = validateSplunkGeneralTerms()
	if err != nil {
		return err
	}

	// if not provided, set default values for imagePullSecrets
	err = ValidateImagePullSecrets(ctx, c, cr, spec)
	if err != nil {
		return err
	}

	if err = validateKVStoreDefaultTypeExtraEnv(spec.ExtraEnv); err != nil {
		return err
	}

	setVolumeDefaults(spec)

	return ValidateSpec(&spec.Spec, splutil.SplunkDefaultResources())
}

func validateKVStoreDefaultTypeExtraEnv(extraEnv []corev1.EnvVar) error {
	for _, env := range extraEnv {
		if env.Name != splunkKVStoreDefaultTypeEnv {
			continue
		}
		if env.Value != splunkKVStoreTypeLocal {
			return fmt.Errorf("%s must be %q", splunkKVStoreDefaultTypeEnv, splunkKVStoreTypeLocal)
		}
	}
	return nil
}

// ValidateImagePullSecrets sets default values for imagePullSecrets if not provided
func ValidateImagePullSecrets(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) error {
	logger := logging.FromContext(ctx).With("func", "ValidateImagePullSecrets")

	// If no imagePullSecrets are configured
	var nilImagePullSecrets []corev1.LocalObjectReference
	if len(spec.ImagePullSecrets) == 0 {
		spec.ImagePullSecrets = nilImagePullSecrets
		return nil
	}

	// If configured, validated if the secret/s exist
	for _, secret := range spec.ImagePullSecrets {
		_, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), secret.Name)
		if err != nil {
			logger.ErrorContext(ctx, "couldn't get secret in the imagePullSecrets config", "Secret", secret.Name, "error", err)
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
	case SplunkIngestor:
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
	var adminManagedPV bool

	annotations := cr.GetAnnotations()

	// determine if CR's PVs are managed by an admin
	if value, ok := annotations["enterprise.splunk.com/admin-managed-pv"]; ok && strings.ToLower(value) == "true" {
		adminManagedPV = true
	}

	volumeClaimTemplate, err := getSplunkVolumeClaims(cr, spec, labels, volumeType, adminManagedPV)
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
func addStorageVolumes(ctx context.Context, cr splcommon.MetaObject, client splcommon.ControllerClient, spec *enterpriseApi.CommonSplunkSpec, statefulSet *appsv1.StatefulSet, labels map[string]string) error {

	logger := logging.FromContext(ctx).With("func", "addStorageVolumes")

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

	// Add Splunk Probe config map
	probeConfigMap, err := getProbeConfigMap(ctx, client, cr)
	if err != nil {
		logger.ErrorContext(ctx, "unable to get probeConfigMap", "error", err)
		return err
	}
	addProbeConfigMapVolume(probeConfigMap, statefulSet)
	return nil
}

func getProbeConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) (*corev1.ConfigMap, error) {

	logger := logging.FromContext(ctx).With("func", "getProbeConfigMap")

	configMapName := GetProbeConfigMapName(cr.GetNamespace())
	configMapNamespace := cr.GetNamespace()
	namespacedName := types.NamespacedName{Namespace: configMapNamespace, Name: configMapName}

	// Check if the config map already exists
	logger.DebugContext(ctx, "checking for existing config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace)
	var configMap corev1.ConfigMap
	err := client.Get(ctx, namespacedName, &configMap)

	if err == nil {
		logger.DebugContext(ctx, "retrieved existing config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace)
		return &configMap, nil
	} else if !k8serrors.IsNotFound(err) {
		logger.ErrorContext(ctx, "error retrieving config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace, "error", err)
		return nil, err
	}

	// Existing config map not found, create one for the probes
	logger.InfoContext(ctx, "creating new config map", "configMapName", configMapName, "configMapNamespace", configMapNamespace)
	configMap = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
		},
	}

	// Add readiness script to config map
	data, err := ReadFile(ctx, GetReadinessScriptLocation())
	if err != nil {
		return &configMap, err
	}
	configMap.Data = map[string]string{GetReadinessScriptName(): data}
	// Add liveness script to config map
	livenessScriptLocation, _ := filepath.Abs(GetLivenessScriptLocation())
	data, err = ReadFile(ctx, livenessScriptLocation)
	if err != nil {
		return &configMap, err
	}
	configMap.Data[GetLivenessScriptName()] = data
	// Add startup script to config map
	startupScriptLocation, _ := filepath.Abs(GetStartupScriptLocation())
	data, err = ReadFile(ctx, startupScriptLocation)
	if err != nil {
		return &configMap, err
	}
	configMap.Data[GetStartupScriptName()] = data

	// Apply the configured config map
	_, err = k8sops.ApplyConfigMap(ctx, client, &configMap)
	if err != nil {
		return &configMap, err
	}
	return &configMap, nil
}

func addProbeConfigMapVolume(configMap *corev1.ConfigMap, statefulSet *appsv1.StatefulSet) {
	configMapVolDefaultMode := GetProbeVolumePermission()
	addSplunkVolumeToTemplate(&statefulSet.Spec.Template, configMap.Name, GetProbeMountDirectory(), corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: configMap.GetName(),
			},
			DefaultMode: &configMapVolDefaultMode,
		},
	})
}

// getSplunkStatefulSet returns a Kubernetes StatefulSet object for Splunk instances configured for a Splunk Enterprise resource.
func getSplunkStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType InstanceType, replicas int32, extraEnv []corev1.EnvVar, certMounts *certs.CertMountConfig, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {

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
				Labels:    labels,
			},
		}
	}

	statefulSet.Spec = appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: selectLabels,
		},
		ServiceName:         splcommon.GetSplunkServiceName(instanceType, cr.GetName(), true),
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
				Affinity:                  affinity,
				Tolerations:               spec.Tolerations,
				TopologySpreadConstraints: spec.TopologySpreadConstraints,
				SchedulerName:             spec.SchedulerName,
				ImagePullSecrets:          spec.ImagePullSecrets,
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
	err = addStorageVolumes(ctx, cr, client, spec, statefulSet, labels)
	if err != nil {
		return statefulSet, err
	}

	// add serviceaccount if configured
	if spec.ServiceAccount != "" {
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: spec.ServiceAccount}
		_, err := k8sops.GetServiceAccount(ctx, client, namespacedName)
		if err == nil {
			// serviceAccount exists
			statefulSet.Spec.Template.Spec.ServiceAccountName = spec.ServiceAccount
		}
	}

	// append labels and annotations from parent
	splcommon.AppendParentMeta(statefulSet.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())
	if len(spec.PodAnnotations) > 0 {
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = make(map[string]string)
		}
		for k, v := range spec.PodAnnotations {
			statefulSet.Spec.Template.Annotations[k] = v
		}
	}

	// retrieve the secret to upload to the statefulSet pod
	statefulSetSecret, err := splutil.GetLatestVersionedSecret(ctx, client, cr, cr.GetNamespace(), statefulSet.GetName())
	if err != nil || statefulSetSecret == nil {
		return statefulSet, err
	}

	// update statefulset's pod template with common splunk pod config
	if err = updateSplunkPodTemplateWithConfig(ctx, client, &statefulSet.Spec.Template, cr, spec, instanceType, extraEnv, statefulSetSecret.GetName()); err != nil {
		return statefulSet, err
	}

	// make Splunk Enterprise object the owner
	statefulSet.SetOwnerReferences(append(statefulSet.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	certs.InjectCertMounts(&statefulSet.Spec.Template, certMounts)

	resources.ApplyStatefulSetOptions(statefulSet, opts...)

	return statefulSet, nil
}

// getSmartstoreConfigMap returns the smartstore configMap, if it exists and applicable for that instanceType
func getSmartstoreConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType) *corev1.ConfigMap {
	var configMap *corev1.ConfigMap

	if instanceType == SplunkStandalone || isCMDeployed(instanceType) {
		smartStoreConfigMapName := GetSplunkSmartstoreConfigMapName(cr.GetName(), cr.GetObjectKind().GroupVersionKind().Kind)
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: smartStoreConfigMapName}
		configMap, _ = k8sops.GetConfigMap(ctx, client, namespacedName)
	}

	return configMap
}

// TODO(SPL-307034): Move this check to `splunk-provision` - it should know which roles it support
// This does not account for unsupported common features - like IPv6, multisite etc.
func splunkProvisionSupportsRole(instanceType InstanceType) bool {
	return instanceType == SplunkSearchHead || instanceType == SplunkDeployer
}

// injectSplunkProvision adds the init container, shared volume, and mounts needed
// to run splunk-provision instead of Ansible. SPLUNK_PROVISION_IMAGE must be set
// in the operator Deployment env (see config/manager/manager.yaml).
func injectSplunkProvision(splunkProvisionImage string, podTemplateSpec *corev1.PodTemplateSpec, extraEnv *[]corev1.EnvVar) {
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, corev1.Volume{
		Name:         "splunk-provision-bin",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, corev1.Container{
		Name:            "splunk-provision-init",
		Image:           splunkProvisionImage,
		ImagePullPolicy: corev1.PullAlways,
		Command: []string{"bash", "-c",
			"cp /opt/splunk-provision/splunk-provision /mnt/splunk-provision/splunk-provision && " +
				"cp /opt/splunk-provision/entrypoint.sh /mnt/splunk-provision/entrypoint.sh && " +
				"chmod 755 /mnt/splunk-provision/splunk-provision /mnt/splunk-provision/entrypoint.sh",
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "splunk-provision-bin", MountPath: "/mnt/splunk-provision"},
		},
	})
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(
			podTemplateSpec.Spec.Containers[idx].VolumeMounts,
			corev1.VolumeMount{
				Name:      "splunk-provision-bin",
				MountPath: "/sbin/entrypoint.sh",
				SubPath:   "entrypoint.sh",
			},
			corev1.VolumeMount{
				Name:      "splunk-provision-bin",
				MountPath: "/opt/splunk/bin/splunk-provision",
				SubPath:   "splunk-provision",
			},
		)
	}
	*extraEnv = append([]corev1.EnvVar{{Name: "SPLUNK_NO_ANSIBLE", Value: "true"}}, *extraEnv...)
}

// updateSplunkPodTemplateWithConfig modifies the podTemplateSpec object based on configuration of the Splunk Enterprise resource.
func updateSplunkPodTemplateWithConfig(ctx context.Context, client splcommon.ControllerClient, podTemplateSpec *corev1.PodTemplateSpec, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec, instanceType InstanceType, extraEnv []corev1.EnvVar, secretToMount string) error {

	logger := logging.FromContext(ctx).With("func", "updateSplunkPodTemplateWithConfig")
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

	// TODO(SPL-306631): remove once the `splunk-provision` is available in the Splunk docker image
	// TODO(SPL-306655): and once the `entrypoint.sh` has been modified in the Splunk docker image
	crAnnotations := cr.GetAnnotations()
	if strings.ToLower(crAnnotations[enterpriseApi.SplunkProvisionAnnotation]) == "true" &&
		splunkProvisionSupportsRole(instanceType) {
		splunkProvisionImage := os.Getenv("SPLUNK_PROVISION_IMAGE")
		if splunkProvisionImage == "" || splunkProvisionImage == "SPLUNK_PROVISION_IMAGE_VALUE" {
			logger.WarnContext(ctx, "skipping splunk-provision injection", "reason", "SPLUNK_PROVISION_IMAGE not set or unresolved placeholder")
		} else {
			logger.Info("injecting splunk-provision as volume via init-container")
			injectSplunkProvision(splunkProvisionImage, podTemplateSpec, &extraEnv)
		}
	}

	// Explicitly set the default value here so we can compare for changes correctly with current statefulset.
	secretVolDefaultMode := corev1.SecretVolumeSourceDefaultMode
	addSplunkVolumeToTemplate(podTemplateSpec, "mnt-splunk-secrets", "/mnt/splunk-secrets", corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName:  secretToMount,
			DefaultMode: &secretVolDefaultMode,
		},
	})

	// Explicitly set the default value here so we can compare for changes correctly with current statefulset.
	configMapVolDefaultMode := corev1.ConfigMapVolumeSourceDefaultMode

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

		// We stamp a content hash of configMap.Data (not ResourceVersion) so that
		// owner-reference-only writes during bootstrap do not trigger pod restarts.
		configMapObj, err := k8sops.GetConfigMap(ctx, client, namespacedName)
		if err == nil {
			podTemplateSpec.ObjectMeta.Annotations["defaultConfigRev"] = configDataHash(configMapObj.Data)
		} else {
			logger.ErrorContext(ctx, "updation of default configMap annotation failed", "error", err)
		}
	}

	// Stamp splcommon.ConfigMapRevAnnotationPrefix+<vol-name> annotation for each user-supplied
	// ConfigMap volume using a content hash rather than ResourceVersion. ResourceVersion changes
	// on any metadata update (labels, annotations) and would cause spurious pod rolls; the hash
	// only changes when the mounted data itself changes.
	// The annotation key uses the volume name (a valid DNS label, ≤63 chars) as the suffix,
	// not the ConfigMap name, which can exceed Kubernetes' 63-char annotation-suffix limit.
	// Projected volumes that reference ConfigMaps are handled via the Sources loop.
	for _, vol := range spec.Volumes {
		switch {
		case vol.ConfigMap != nil:
			cmNS := types.NamespacedName{Namespace: cr.GetNamespace(), Name: vol.ConfigMap.Name}
			cm, err := k8sops.GetConfigMap(ctx, client, cmNS)
			if err != nil {
				logger.ErrorContext(ctx, "Failed to fetch ConfigMap for restart annotation", "volume", vol.Name, "error", err)
				break
			}
			if cm.Annotations[splcommon.ConfigMapRestartOptOutAnnotation] == "false" {
				// Consumer handles dynamic reload; skip the restart-triggering annotation.
				break
			}
			hash, err := k8sops.GetConfigMapDataHash(ctx, client, cmNS, vol.ConfigMap.Items)
			if err == nil {
				podTemplateSpec.ObjectMeta.Annotations[splcommon.ConfigMapRevAnnotationPrefix+vol.Name] = hash
			} else {
				logger.ErrorContext(ctx, "Failed to get ConfigMap data hash for annotation", "volume", vol.Name, "error", err)
			}
		case vol.Projected != nil:
			for i, src := range vol.Projected.Sources {
				if src.ConfigMap == nil {
					continue
				}
				cmNS := types.NamespacedName{Namespace: cr.GetNamespace(), Name: src.ConfigMap.Name}
				cm, err := k8sops.GetConfigMap(ctx, client, cmNS)
				if err != nil {
					logger.ErrorContext(ctx, "Failed to fetch projected ConfigMap for restart annotation", "volume", vol.Name, "configMap", src.ConfigMap.Name, "error", err)
					continue
				}
				if cm.Annotations[splcommon.ConfigMapRestartOptOutAnnotation] == "false" {
					continue
				}
				hash, err := k8sops.GetConfigMapDataHash(ctx, client, cmNS, src.ConfigMap.Items)
				if err == nil {
					// Build a collision-free annotation key suffix ≤63 chars.
					// vol.Name is a DNS label (≤63 chars); appending ".<n>" can push past the
					// Kubernetes annotation name-segment limit. When the combined length exceeds
					// 63, replace vol.Name with "p.<8-hex-digest>" — the "p." prefix contains a
					// dot, which is legal in annotation name segments but cannot appear in a
					// Kubernetes DNS-label volume name, making hashed keys structurally distinct
					// from any real short volume name and preventing false collisions.
					idxStr := strconv.Itoa(i)
					volNamePart := vol.Name
					if len(volNamePart)+1+len(idxStr) > 63 {
						sum := sha256.Sum256([]byte(vol.Name))
						volNamePart = "p." + hex.EncodeToString(sum[:])[:8]
					}
					podTemplateSpec.ObjectMeta.Annotations[splcommon.ConfigMapRevAnnotationPrefix+volNamePart+"."+idxStr] = hash
				} else {
					logger.ErrorContext(ctx, "Failed to get ConfigMap data hash for projected annotation", "volume", vol.Name, "configMap", src.ConfigMap.Name, "error", err)
				}
			}
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
		// 2. In case of Standalone, reset the Pod by updating the content hash of the
		// smartstore config map so that only real data changes trigger a pod restart.
		if instanceType == SplunkStandalone {
			podTemplateSpec.ObjectMeta.Annotations[smartStoreConfigRev] = configDataHash(smartstoreConfigMap.Data)
		}
	}

	// update security context
	runAsUser := int64(41812)
	fsGroup := int64(41812)
	runAsNonRoot := true
	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	podTemplateSpec.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser:           &runAsUser,
		FSGroup:             &fsGroup,
		RunAsNonRoot:        &runAsNonRoot,
		FSGroupChangePolicy: &fsGroupChangePolicy,
	}

	livenessProbe := getLivenessProbe(ctx, cr, instanceType, spec)
	readinessProbe := getReadinessProbe(ctx, cr, instanceType, spec)
	startupProbe := getStartupProbe(ctx, cr, instanceType, spec)

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
	domainName := os.Getenv("CLUSTER_DOMAIN")
	if domainName == "" {
		domainName = "cluster.local"
	}
	env := []corev1.EnvVar{
		{Name: "SPLUNK_HOME", Value: "/opt/splunk"},
		{Name: "SPLUNK_START_ARGS", Value: "--accept-license"},
		{Name: "SPLUNK_DEFAULTS_URL", Value: splunkDefaults},
		{Name: "SPLUNK_HOME_OWNERSHIP_ENFORCEMENT", Value: "false"},
		{Name: "SPLUNK_ROLE", Value: role},
		{Name: "SPLUNK_DECLARATIVE_ADMIN_PASSWORD", Value: "true"},
		{Name: livenessProbeDriverPathEnv, Value: GetLivenessDriverFilePath()},
		{Name: "SPLUNK_GENERAL_TERMS", Value: os.Getenv("SPLUNK_GENERAL_TERMS")},
		{Name: "SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH", Value: "true"},
		{Name: "SPLUNK_NODE_SIDECAR_POSTGRES_DISABLED", Value: "true"},
	}
	if instanceType != SplunkIngestor {
		env = append(env, corev1.EnvVar{Name: splunkKVStoreDefaultTypeEnv, Value: splunkKVStoreTypeLocal})
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
	}
	if os.Getenv("SPLUNK_HEC_SSL") == "false" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_HEC_SSL",
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
	if instanceType != SplunkLicenseManager && spec.LicenseManagerRef.Name != "" {
		licenseManagerURL := splcommon.GetSplunkServiceName(SplunkLicenseManager, spec.LicenseManagerRef.Name, false)
		if spec.LicenseManagerRef.Namespace != "" {
			licenseManagerURL = splcommon.GetServiceFQDN(spec.LicenseManagerRef.Namespace, licenseManagerURL)
		}
		env = append(env, corev1.EnvVar{
			Name:  splcommon.LicenseManagerURL,
			Value: licenseManagerURL,
		})
	} else if instanceType != SplunkLicenseMaster && spec.LicenseMasterRef.Name != "" {
		licenseMasterURL := splcommon.GetSplunkServiceName(SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
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
		clusterManagerURL = splcommon.GetSplunkServiceName(SplunkClusterManager, spec.ClusterManagerRef.Name, false)
		if spec.ClusterManagerRef.Namespace != "" {
			clusterManagerURL = splcommon.GetServiceFQDN(spec.ClusterManagerRef.Namespace, clusterManagerURL)
		}
		if spec.LicenseManagerRef.Name == "" && spec.LicenseMasterRef.Name == "" {
			//Check if CM is connected to a LicenseManager
			cmNamespace := cr.GetNamespace()
			if spec.ClusterManagerRef.Namespace != "" {
				cmNamespace = spec.ClusterManagerRef.Namespace
			}
			namespacedName := types.NamespacedName{
				Namespace: cmNamespace,
				Name:      spec.ClusterManagerRef.Name,
			}
			managerIdxCluster := &enterpriseApi.ClusterManager{}
			err := client.Get(ctx, namespacedName, managerIdxCluster)
			if err != nil {
				// Return the error so the reconcile loop requeues rather than continuing
				// with a zero-value CR (which would produce an incomplete env and cause a
				// spurious pod restart on the next reconcile when the real value is found).
				logger.ErrorContext(ctx, "unable to get ClusterManager; requeueing", "error", err)
				return err
			}

			if managerIdxCluster.Spec.LicenseManagerRef.Name != "" {
				licenseManagerNamespace := managerIdxCluster.Spec.LicenseManagerRef.Namespace
				if licenseManagerNamespace == "" {
					licenseManagerNamespace = managerIdxCluster.GetNamespace()
				}
				licenseManagerURL := splcommon.GetSplunkServiceName(SplunkLicenseManager, managerIdxCluster.Spec.LicenseManagerRef.Name, false)
				licenseManagerURL = splcommon.GetServiceFQDN(licenseManagerNamespace, licenseManagerURL)
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseManagerURL,
				})
			} else if managerIdxCluster.Spec.LicenseMasterRef.Name != "" {
				licenseMasterNamespace := managerIdxCluster.Spec.LicenseMasterRef.Namespace
				if licenseMasterNamespace == "" {
					licenseMasterNamespace = managerIdxCluster.GetNamespace()
				}
				licenseMasterURL := splcommon.GetSplunkServiceName(SplunkLicenseMaster, managerIdxCluster.Spec.LicenseMasterRef.Name, false)
				licenseMasterURL = splcommon.GetServiceFQDN(licenseMasterNamespace, licenseMasterURL)
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseMasterURL,
				})
			}
		}
	} else if spec.ClusterMasterRef.Name != "" {
		clusterManagerURL = splcommon.GetSplunkServiceName(SplunkClusterMaster, spec.ClusterMasterRef.Name, false)
		if spec.ClusterMasterRef.Namespace != "" {
			clusterManagerURL = splcommon.GetServiceFQDN(spec.ClusterMasterRef.Namespace, clusterManagerURL)
		}
		if spec.LicenseManagerRef.Name == "" && spec.LicenseMasterRef.Name == "" {
			//Check if CM is connected to a LicenseManager
			cmNamespace := cr.GetNamespace()
			if spec.ClusterMasterRef.Namespace != "" {
				cmNamespace = spec.ClusterMasterRef.Namespace
			}
			namespacedName := types.NamespacedName{
				Namespace: cmNamespace,
				Name:      spec.ClusterMasterRef.Name,
			}
			managerIdxCluster := &enterpriseApiV3.ClusterMaster{}
			err := client.Get(ctx, namespacedName, managerIdxCluster)
			if err != nil {
				// Return the error so the reconcile loop requeues rather than continuing
				// with a zero-value CR (which would produce an incomplete env and cause a
				// spurious pod restart on the next reconcile when the real value is found).
				logger.ErrorContext(ctx, "unable to get ClusterMaster; requeueing", "error", err)
				return err
			}

			if managerIdxCluster.Spec.LicenseManagerRef.Name != "" {
				licenseManagerNamespace := managerIdxCluster.Spec.LicenseManagerRef.Namespace
				if licenseManagerNamespace == "" {
					licenseManagerNamespace = managerIdxCluster.GetNamespace()
				}
				licenseManagerURL := splcommon.GetSplunkServiceName(SplunkLicenseManager, managerIdxCluster.Spec.LicenseManagerRef.Name, false)
				licenseManagerURL = splcommon.GetServiceFQDN(licenseManagerNamespace, licenseManagerURL)
				env = append(env, corev1.EnvVar{
					Name:  splcommon.LicenseManagerURL,
					Value: licenseManagerURL,
				})
			} else if managerIdxCluster.Spec.LicenseMasterRef.Name != "" {
				licenseMasterNamespace := managerIdxCluster.Spec.LicenseMasterRef.Namespace
				if licenseMasterNamespace == "" {
					licenseMasterNamespace = managerIdxCluster.GetNamespace()
				}
				licenseMasterURL := splcommon.GetSplunkServiceName(SplunkLicenseMaster, managerIdxCluster.Spec.LicenseMasterRef.Name, false)
				licenseMasterURL = splcommon.GetServiceFQDN(licenseMasterNamespace, licenseMasterURL)
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
	extraEnv = append(spec.ExtraEnv, extraEnv...)

	// append any extra variables adding environment variable from extraEnv in the first
	// so when duplicates are removed the last ones are removed from the list
	env = append(extraEnv, env...)
	//env = append(env, extraEnv...)

	// check if there are any duplicate entries
	// we use orderedmap so the test case can pass as json marshal
	// expects order
	if len(env) > 0 {
		env = removeDuplicateEnvVars(env)
	}

	privileged := false
	// update each container in pod
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].Resources = spec.Resources
		podTemplateSpec.Spec.Containers[idx].LivenessProbe = livenessProbe
		podTemplateSpec.Spec.Containers[idx].ReadinessProbe = readinessProbe
		podTemplateSpec.Spec.Containers[idx].StartupProbe = startupProbe
		podTemplateSpec.Spec.Containers[idx].Env = env
		podTemplateSpec.Spec.Containers[idx].SecurityContext = &corev1.SecurityContext{
			RunAsUser:                &runAsUser,
			RunAsNonRoot:             &runAsNonRoot,
			AllowPrivilegeEscalation: &[]bool{false}[0],
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
				Add: []corev1.Capability{
					"NET_BIND_SERVICE",
				},
			},
			Privileged: &privileged,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		}
	}
	return nil
}

func removeDuplicateEnvVars(sliceList []corev1.EnvVar) []corev1.EnvVar {
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

// getLivenessProbe the probe for checking the liveness of the Pod
func getLivenessProbe(ctx context.Context, cr splcommon.MetaObject, instanceType InstanceType, spec *enterpriseApi.CommonSplunkSpec) *corev1.Probe {
	logger := logging.FromContext(ctx)
	livenessProbe := getProbeWithConfigUpdates(&defaultLivenessProbe, spec.LivenessProbe, spec.LivenessInitialDelaySeconds)
	logger.DebugContext(ctx, "livenessProbe", "Configured", livenessProbe)
	return livenessProbe
}

// getReadinessProbe the probe for checking the readiness of the Pod
func getReadinessProbe(ctx context.Context, cr splcommon.MetaObject, instanceType InstanceType, spec *enterpriseApi.CommonSplunkSpec) *corev1.Probe {
	logger := logging.FromContext(ctx)
	readinessProbe := getProbeWithConfigUpdates(&defaultReadinessProbe, spec.ReadinessProbe, spec.ReadinessInitialDelaySeconds)
	logger.DebugContext(ctx, "readinessProbe", "Configured", readinessProbe)
	return readinessProbe
}

// getStartupProbe the probe for checking the first start of splunk on the Pod
func getStartupProbe(ctx context.Context, cr splcommon.MetaObject, instanceType InstanceType, spec *enterpriseApi.CommonSplunkSpec) *corev1.Probe {
	logger := logging.FromContext(ctx)
	startupProbe := getProbeWithConfigUpdates(&defaultStartupProbe, spec.StartupProbe, 0)
	logger.DebugContext(ctx, "startupProbe", "Configured", startupProbe)
	return startupProbe
}

// getProbeWithConfigUpdates Validates probe values and updates them
func getProbeWithConfigUpdates(defaultProbe *corev1.Probe, configuredProbe *enterpriseApi.Probe, configuredDelay int32) *corev1.Probe {
	if configuredProbe != nil {
		// Always take a separate probe, instead of referring the memory address from spec.
		// (Referring the configured Probe memory is kind of OK as we are not writing to the DB, however
		// updating any values(if the Application needs to do) can cause confusion when referring the CR
		// while handling a reconcile event)
		//var derivedProbe = *configuredProbe
		derivedProbe := corev1.Probe{
			InitialDelaySeconds: configuredProbe.InitialDelaySeconds,
			TimeoutSeconds:      configuredProbe.TimeoutSeconds,
			PeriodSeconds:       configuredProbe.PeriodSeconds,
			FailureThreshold:    configuredProbe.FailureThreshold,
		}

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
		// CSPL-2242 - Default value for FailureThreshold not being set forces unnecessary statefulSet updates
		if derivedProbe.FailureThreshold == 0 {
			derivedProbe.FailureThreshold = defaultProbe.FailureThreshold
		}
		// Always use defaultProbe Exec. At this time customer supported scripts are not supported.
		derivedProbe.Exec = defaultProbe.Exec
		return &derivedProbe
	} else if configuredDelay != 0 {
		var derivedProbe = *defaultProbe
		derivedProbe.InitialDelaySeconds = configuredDelay
		return &derivedProbe
	}
	return defaultProbe
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

	logger := logging.FromContext(ctx).With("func", "AreRemoteVolumeKeysChanged")

	volList := smartstore.VolList
	for _, volume := range volList {
		if volume.SecretRef != "" {
			namespaceScopedSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), volume.SecretRef)
			// Ideally, this should have been detected in Spec validation time
			if err != nil {
				*retError = fmt.Errorf("not able to access secret object = %s, reason: %s", volume.SecretRef, err)
				return false
			}

			// Check if the secret version is already tracked, and if there is a change in it
			if existingSecretVersion, ok := ResourceRev[volume.SecretRef]; ok {
				if existingSecretVersion != namespaceScopedSecret.ResourceVersion {
					logger.InfoContext(ctx, "secret keys changed", "previousResourceVersion", existingSecretVersion, "currentVersion", namespaceScopedSecret.ResourceVersion)
					ResourceRev[volume.SecretRef] = namespaceScopedSecret.ResourceVersion
					return true
				}
				return false
			}

			// First time adding to track the secret resource version
			ResourceRev[volume.SecretRef] = namespaceScopedSecret.ResourceVersion
		} else {
			logger.DebugContext(ctx, "no valid SecretRef for volume. No secret to track", "volumeName", volume.Name)
		}
	}

	return false
}

// ApplyManualAppUpdateConfigMap applies the manual app update config map
func ApplyManualAppUpdateConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, crKindMap map[string]string) (*corev1.ConfigMap, error) {

	logger := logging.FromContext(ctx).With("func", "ApplyManualAppUpdateConfigMap")

	configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	var configMap *corev1.ConfigMap
	var err error
	var newConfigMap bool
	configMap, err = k8sops.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		configMap = k8sops.PrepareConfigMap(configMapName, cr.GetNamespace(), crKindMap)
		newConfigMap = true
	}

	configMap.Data = crKindMap

	// set this CR as owner reference for the configMap
	configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	if newConfigMap {
		logger.InfoContext(ctx, "creating manual app update configMap")
		err = splutil.CreateResource(ctx, client, configMap)
		if err != nil {
			logger.ErrorContext(ctx, "unable to create the configMap", "name", configMapName, "error", err)
			return configMap, err
		}
	} else {
		logger.InfoContext(ctx, "updating manual app update configMap")
		err = splutil.UpdateResource(ctx, client, configMap)
		if err != nil {
			logger.ErrorContext(ctx, "unable to update the configMap", "name", configMapName, "error", err)
			return configMap, err
		}
	}
	return configMap, nil
}

// getManualUpdateStatus extracts the status field from the configMap data
func getManualUpdateStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) string {
	logger := logging.FromContext(ctx).With("func", "getManualUpdateStatus")

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
	result := ""
	if err == nil {
		statusRegex := ".*status: (?P<status>.*).*"
		data := configMap.Data[cr.GetObjectKind().GroupVersionKind().Kind]
		result = extractFieldFromConfigMapData(statusRegex, data)
		if result == "on" {
			logger.InfoContext(ctx, "namespace configMap value is set to", "name", configMapName, "data", result)
			return result
		}
	} else {
		logger.ErrorContext(ctx, "unable to get namespace specific configMap", "name", configMapName, "error", err)
	}

	return "off"
}

// getManualUpdatePerCrStatus extracts the status field from the configMap data
func getManualUpdatePerCrStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) string {
	logger := logging.FromContext(ctx).With("func", "getManualUpdatePerCrStatus")

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: fmt.Sprintf(perCrConfigMapNameStr, KindToInstanceString(cr.GroupVersionKind().Kind), cr.GetName())}
	crconfigMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
	if err == nil {
		logger.InfoContext(ctx, "custom configMap value is set to", "name", configMapName, "data", crconfigMap.Data)
		data := crconfigMap.Data["manualUpdate"]
		return data
	} else {
		logger.ErrorContext(ctx, "unable to get custom specific configMap", "name", configMapName, "error", err)
	}

	return "off"
}

// getManualUpdateRefCount extracts the refCount field from the configMap data
func getManualUpdateRefCount(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) int {
	logger := logging.FromContext(ctx).With("func", "getManualUpdateRefCount")
	var refCount int
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		logger.ErrorContext(ctx, "unable to get the configMap", "name", configMapName, "error", err)
		return refCount
	}

	refCountRegex := ".*refCount: (?P<refCount>.*).*"
	data := configMap.Data[cr.GetObjectKind().GroupVersionKind().Kind]

	refCount, _ = strconv.Atoi(extractFieldFromConfigMapData(refCountRegex, data))
	return refCount
}

// createOrUpdateAppUpdateConfigMap creates or updates the manual app update configMap
func createOrUpdateAppUpdateConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) (*corev1.ConfigMap, error) {
	logger := logging.FromContext(ctx).With("func", "createOrUpdateAppUpdateConfigMap", "name", cr.GetName(), "namespace", cr.GetNamespace())

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
	configMap, err = k8sops.GetConfigMap(ctx, client, namespacedName)
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

		logger.InfoContext(ctx, "existing configMap data", "data", configMap.Data)
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
		logger.ErrorContext(ctx, "create/update configMap for app update failed", "error", err)
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
		if _, ok := splstorage.RemoteDataClientsMap[vol.Provider]; !ok {
			splstorage.RegisterRemoteDataClient(ctx, vol.Provider)
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
	return scope == enterpriseApi.ScopeLocal || scope == enterpriseApi.ScopeCluster || scope == enterpriseApi.ScopePremiumApps || scope == enterpriseApi.ScopeClusterWithPreConfig
}

// validateSplunkAppSources validates the App source config in App Framework spec
func validateSplunkAppSources(appFramework *enterpriseApi.AppFrameworkSpec, localOrPremScope bool, crKind string) error {

	duplicateAppSourceStorageChecker := make(map[string]map[string]bool)
	duplicateAppSourceStorageChecker[enterpriseApi.ScopeLocal] = make(map[string]bool)
	duplicateAppSourceStorageChecker[enterpriseApi.ScopePremiumApps] = make(map[string]bool)

	// CSPL-2574 - Assign just in case invalid scope is passed through!
	duplicateAppSourceStorageChecker[enterpriseApi.ScopeCluster] = make(map[string]bool)
	duplicateAppSourceStorageChecker[enterpriseApi.ScopeClusterWithPreConfig] = make(map[string]bool)

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
			_, err := splutil.CheckIfVolumeExists(appFramework.VolList, appSrc.VolName)
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
			if localOrPremScope && !(appSrc.Scope == enterpriseApi.ScopeLocal || appSrc.Scope == enterpriseApi.ScopePremiumApps) {
				return fmt.Errorf("invalid scope for App Source: %s. Valid scopes are %s or %s for this kind of CR", appSrc.Name, enterpriseApi.ScopeLocal, enterpriseApi.ScopePremiumApps)
			}

			if !isAppSourceScopeValid(appSrc.Scope) {
				return fmt.Errorf("scope for App Source: %s should be either %s or %s or %s", appSrc.Name, enterpriseApi.ScopeLocal, enterpriseApi.ScopeCluster, enterpriseApi.ScopePremiumApps)
			}

			// Check for premium apps properties
			if appSrc.Scope == enterpriseApi.ScopePremiumApps || appFramework.Defaults.Scope == enterpriseApi.ScopePremiumApps {
				err := validatePremiumAppsInputs(appSrc, crKind)
				if err != nil {
					return err
				}
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

	if localOrPremScope && appFramework.Defaults.Scope != "" &&
		(appFramework.Defaults.Scope != enterpriseApi.ScopeLocal && appFramework.Defaults.Scope != enterpriseApi.ScopePremiumApps) {
		return fmt.Errorf("invalid scope for defaults config. Only local scope is supported for this kind of CR")
	}

	if appFramework.Defaults.Scope != "" && !isAppSourceScopeValid(appFramework.Defaults.Scope) {
		return fmt.Errorf("scope for defaults should be either local Or cluster, but configured as: %s", appFramework.Defaults.Scope)
	}

	if appFramework.Defaults.VolName != "" {
		_, err := splutil.CheckIfVolumeExists(appFramework.VolList, appFramework.Defaults.VolName)
		if err != nil {
			return fmt.Errorf("invalid Volume Name for Defaults. Error: %s", err)
		}
	}

	return nil
}

// validatePremiumAppsInputs validates premium app source spec
func validatePremiumAppsInputs(appSrc enterpriseApi.AppSourceSpec, crKind string) error {

	if appSrc.AppSourceDefaultSpec.PremiumAppsProps.Type != enterpriseApi.PremiumAppsTypeEs {
		return fmt.Errorf("invalid PremiumAppsProps. Valid value is %s", enterpriseApi.PremiumAppsTypeEs)
	}

	// Check sslEnablement in ES defaults
	sslEnablementValue := appSrc.AppSourceDefaultSpec.PremiumAppsProps.EsDefaults.SslEnablement
	if sslEnablementValue != "" && !(sslEnablementValue == enterpriseApi.SslEnablementAuto ||
		sslEnablementValue == enterpriseApi.SslEnablementIgnore ||
		sslEnablementValue == enterpriseApi.SslEnablementStrict) {
		return fmt.Errorf("invalid sslEnablement. Valid values are %s or %s or %s", enterpriseApi.SslEnablementAuto,
			enterpriseApi.SslEnablementIgnore, enterpriseApi.SslEnablementStrict)
	}

	// SHC ES app cannot use ssl_enablement auto, product doesn't support it
	if crKind == "SearchHeadCluster" {
		if appSrc.PremiumAppsProps.Type == enterpriseApi.PremiumAppsTypeEs {
			if appSrc.AppSourceDefaultSpec.PremiumAppsProps.EsDefaults.SslEnablement == enterpriseApi.SslEnablementAuto {
				return fmt.Errorf("scope for app source: %s search head cluster cannot have an ES app installed with ssl_enablement auto", appSrc.Name)
			}
		}
	}
	return nil
}

// isAppFrameworkConfigured checks and returns true if App Framework is configured
// App Repo config without any App sources will not cause any App Framework activity
func isAppFrameworkConfigured(appFramework *enterpriseApi.AppFrameworkSpec) bool {
	return !(appFramework == nil || appFramework.AppSources == nil)
}

// ValidateAppFrameworkSpec checks and validates the Apps Frame Work config
func ValidateAppFrameworkSpec(ctx context.Context, appFramework *enterpriseApi.AppFrameworkSpec, appContext *enterpriseApi.AppDeploymentContext, localScope bool, crKind string) error {
	var err error
	if !isAppFrameworkConfigured(appFramework) {
		return nil
	}

	logger := logging.FromContext(ctx).With("func", "ValidateAppFrameworkSpec")

	logger.InfoContext(ctx, "configCheck", "scope", localScope)

	// Set the value in status field to be same as that in spec.
	appContext.AppsRepoStatusPollInterval = appFramework.AppsRepoPollInterval
	appContext.AppsStatusMaxConcurrentAppDownloads = appFramework.MaxConcurrentAppDownloads

	if appContext.AppsRepoStatusPollInterval <= 0 {
		logger.ErrorContext(ctx, "appsRepoPollIntervalSeconds is not configured. Disabling polling of apps repo changes, defaulting to manual updates", "error", err)
		appContext.AppsRepoStatusPollInterval = 0
	} else if appFramework.AppsRepoPollInterval < splcommon.MinAppsRepoPollInterval {
		logger.ErrorContext(ctx, "configured appsRepoPollIntervalSeconds is too small", "error", err, "configuredValue", appFramework.AppsRepoPollInterval, "defaultMinSeconds", splcommon.MinAppsRepoPollInterval)
		appContext.AppsRepoStatusPollInterval = splcommon.MinAppsRepoPollInterval
	} else if appFramework.AppsRepoPollInterval > splcommon.MaxAppsRepoPollInterval {
		logger.ErrorContext(ctx, "configured appsRepoPollIntervalSeconds is too large", "error", err, "configuredValue", appFramework.AppsRepoPollInterval, "defaultMaxSeconds", splcommon.MaxAppsRepoPollInterval)
		appContext.AppsRepoStatusPollInterval = splcommon.MaxAppsRepoPollInterval
	}

	if appContext.AppsStatusMaxConcurrentAppDownloads <= 0 {
		logger.InfoContext(ctx, "invalid value of maxConcurrentAppDownloads", "configuredValue", appContext.AppsStatusMaxConcurrentAppDownloads, "defaultValue", splcommon.DefaultMaxConcurrentAppDownloads)
		appContext.AppsStatusMaxConcurrentAppDownloads = splcommon.DefaultMaxConcurrentAppDownloads
	}

	// check whether the temporary volume to download apps is mounted or not on the operator pod;
	// use the resolved path (which may have fallen back to TmpAppDownloadDir) rather than the
	// configured const, since a missing mount is expected to fall back, not fail validation.
	appDownloadVolume := getResolvedAppDownloadVolume()
	if _, err := os.Stat(appDownloadVolume); errors.Is(err, os.ErrNotExist) {
		logger.ErrorContext(ctx, "volume needs to be mounted on operator pod to download apps. Please mount it as a separate volume on operator pod", "error", err, "volumePath", appDownloadVolume)
		return err
	}

	err = validateRemoteVolumeSpec(ctx, appFramework.VolList, true)
	if err != nil {
		return err
	}

	err = validateSplunkAppSources(appFramework, localScope, crKind)
	if err == nil {
		logger.InfoContext(ctx, "app framework configuration is valid")
	}

	return err
}

// validateRemoteVolumeSpec validates the Remote storage volume spec
func validateRemoteVolumeSpec(ctx context.Context, volList []enterpriseApi.VolumeSpec, isAppFramework bool) error {

	duplicateChecker := make(map[string]bool)

	logger := logging.FromContext(ctx).With("func", "validateRemoteVolumeSpec")

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
			logger.InfoContext(ctx, "no valid SecretRef for volume", "volumeName", volume.Name)
		}

		// provider is used in App framework to pick the S3 client(supported providers are aws and minio),
		// or Blob client (supported provider is azure) and is not applicable to Smartstore
		// For now, Smartstore supports only S3, which is by default.
		if isAppFramework {
			if !isValidStorageType(volume.Type) {
				return fmt.Errorf("storageType '%s' is invalid. Valid values are 's3', 'gcs' and 'blob'", volume.Type)
			}

			if !isValidProvider(volume.Provider) {
				return fmt.Errorf("provider '%s' is invalid. Valid values are 'aws', 'minio', 'gcp' and 'azure'", volume.Provider)
			}

			if !isValidProviderForStorageType(volume.Type, volume.Provider) {
				return fmt.Errorf("storageType '%s' cannot be used with provider '%s'. Valid combinations are (s3,aws), (s3,minio), (gcs,gcp) and (blob,azure)", volume.Type, volume.Provider)
			}
		}
	}
	return nil
}

// isValidStorageType checks if the storage type specified is valid and supported
func isValidStorageType(storage string) bool {
	return storage != "" && (storage == "s3" || storage == "blob" || storage == "gcs")
}

// isValidProvider checks if the provider specified is valid and supported
func isValidProvider(provider string) bool {
	return provider != "" && (provider == "aws" || provider == "minio" || provider == "azure" || provider == "gcp")
}

// Valid provider for s3 are aws and minio
// Valid provider for blob is azure
func isValidProviderForStorageType(storageType string, provider string) bool {
	return ((storageType == "s3" && (provider == "aws" || provider == "minio")) ||
		(storageType == "blob" && provider == "azure") ||
		(storageType == "gcs" && provider == "gcp"))
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
			_, err := splutil.CheckIfVolumeExists(smartstore.VolList, index.VolName)
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
		_, err = splutil.CheckIfVolumeExists(smartstore.VolList, defaults.VolName)
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

	logger := logging.FromContext(ctx).With("func", "GetSmartstoreVolumesConfig")

	volumes := smartstore.VolList
	for i := 0; i < len(volumes); i++ {
		if volumes[i].SecretRef != "" {
			s3AccessKey, s3SecretKey, _, err := GetSmartstoreRemoteVolumeSecrets(ctx, volumes[i], client, cr, smartstore)
			if err != nil {
				return "", fmt.Errorf("unable to read the secrets for volume = %s. %s", volumes[i].Name, err)
			}

			volumesConf = fmt.Sprintf(`%s
[volume:%s]
storageType = remote
path = s3://%s
remote.s3.access_key = %s
remote.s3.secret_key = %s
remote.s3.endpoint = %s
remote.s3.auth_region = %s
`, volumesConf, volumes[i].Name, volumes[i].Path, s3AccessKey, s3SecretKey, volumes[i].Endpoint, volumes[i].Region)
		} else {
			logger.InfoContext(ctx, "no valid secretRef configured.  Configure volume without access/secret keys", "volumeName", volumes[i].Name)
			volumesConf = fmt.Sprintf(`%s
[volume:%s]
storageType = remote
path = s3://%s
remote.s3.endpoint = %s
remote.s3.auth_region = %s
`, volumesConf, volumes[i].Name, volumes[i].Path, volumes[i].Endpoint, volumes[i].Region)
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

		// Add a new line in between index stanzas
		// Do not add config beyond here
		indexesConf = fmt.Sprintf(`%s
`, indexesConf)
	}

	return indexesConf
}

// GetServerConfigEntries prepares the server.conf entries, and returns as a string
func GetServerConfigEntries(cacheManagerConf *enterpriseApi.CacheManagerSpec) string {
	if cacheManagerConf == nil {
		return ""
	}

	var serverConfIni string
	serverConfIni = `[cachemanager]`

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

// validateProbe validates a generic probe values
func validateProbe(probe *enterpriseApi.Probe) error {
	if probe.InitialDelaySeconds < 0 || probe.TimeoutSeconds < 0 || probe.PeriodSeconds < 0 || probe.FailureThreshold < 0 {
		return fmt.Errorf("negative values are not allowed. Configured values InitialDelaySeconds = %d, TimeoutSeconds = %d, PeriodSeconds = %d, FailureThreshold = %d", probe.InitialDelaySeconds, probe.TimeoutSeconds, probe.PeriodSeconds, probe.FailureThreshold)
	}
	return nil
}

// validateLivenessProbe validates the liveness probe config
func validateLivenessProbe(ctx context.Context, cr splcommon.MetaObject, livenessProbe *enterpriseApi.Probe) error {
	var err error
	logger := logging.FromContext(ctx).With("func", "validateLivenessProbe", "name", cr.GetName(), "namespace", cr.GetNamespace())

	if livenessProbe == nil {
		logger.InfoContext(ctx, "empty liveness probe")
		return err
	}

	err = validateProbe(livenessProbe)
	if err != nil {
		return fmt.Errorf("invalid Liveness Probe config. Reason: %s", err)
	}

	if livenessProbe.InitialDelaySeconds != 0 && livenessProbe.InitialDelaySeconds < livenessProbeDefaultDelaySec {
		logger.InfoContext(ctx, "liveness Probe: Configured  InitialDelaySeconds is too small, recommended default minimum will be used", "configured", livenessProbe.InitialDelaySeconds, "recommendedMinimum", livenessProbeDefaultDelaySec)
	}

	if livenessProbe.TimeoutSeconds != 0 && livenessProbe.TimeoutSeconds < livenessProbeTimeoutSec {
		logger.InfoContext(ctx, "liveness Probe: Configured TimeoutSeconds is too small, recommended default minimum will be used", "configured", livenessProbe.TimeoutSeconds, "recommendedMinimum", livenessProbeTimeoutSec)
	}

	if livenessProbe.PeriodSeconds != 0 && livenessProbe.PeriodSeconds < livenessProbePeriodSec {
		logger.InfoContext(ctx, "liveness Probe: Configured PeriodSeconds is too small, recommended default minimum will be used", "configured", livenessProbe.PeriodSeconds, "recommendedMinimum", livenessProbePeriodSec)
	}

	if livenessProbe.FailureThreshold != 0 && livenessProbe.FailureThreshold < livenessProbeFailureThreshold {
		logger.InfoContext(ctx, "liveness Probe: Configured FailureThreshold is too small, recommended default minimum will be used", "configured", livenessProbe.FailureThreshold, "recommendedMinimum", livenessProbeFailureThreshold)
	}

	return err
}

// validateReadinessProbe validates the Readiness probe config
func validateReadinessProbe(ctx context.Context, cr splcommon.MetaObject, readinessProbe *enterpriseApi.Probe) error {
	var err error
	logger := logging.FromContext(ctx).With("func", "validateReadinessProbe", "name", cr.GetName(), "namespace", cr.GetNamespace())

	if readinessProbe == nil {
		logger.InfoContext(ctx, "empty readiness probe")
		return err
	}

	err = validateProbe(readinessProbe)
	if err != nil {
		return fmt.Errorf("invalid Readiness Probe config. Reason: %s", err)
	}

	if readinessProbe.InitialDelaySeconds != 0 && readinessProbe.InitialDelaySeconds < readinessProbeDefaultDelaySec {
		logger.InfoContext(ctx, "readiness Probe: Configured InitialDelaySeconds is too small, recommended default minimum will be used", "configured", readinessProbe.InitialDelaySeconds, "recommendedMinimum", readinessProbeDefaultDelaySec)
	}

	if readinessProbe.TimeoutSeconds != 0 && readinessProbe.TimeoutSeconds < readinessProbeTimeoutSec {
		logger.InfoContext(ctx, "readiness Probe: Configured TimeoutSeconds is too small, recommended default minimum will be used", "configured", readinessProbe.TimeoutSeconds, "recommendedMinimum", readinessProbeTimeoutSec)
	}

	if readinessProbe.PeriodSeconds != 0 && readinessProbe.PeriodSeconds < readinessProbePeriodSec {
		logger.InfoContext(ctx, "readiness Probe: Configured PeriodSeconds is too small, recommended default minimum will be used", "configured", readinessProbe.PeriodSeconds, "recommendedMinimum", readinessProbePeriodSec)
	}

	if readinessProbe.FailureThreshold != 0 && readinessProbe.FailureThreshold < readinessProbeFailureThreshold {
		logger.InfoContext(ctx, "readiness Probe: Configured FailureThreshold is too small, recommended default minimum will be used", "configured", readinessProbe.FailureThreshold, "recommendedMinimum", readinessProbeFailureThreshold)
	}
	return err
}

// validateStartupProbe validates the startup probe config
func validateStartupProbe(ctx context.Context, cr splcommon.MetaObject, startupProbe *enterpriseApi.Probe) error {
	var err error
	logger := logging.FromContext(ctx).With("func", "validateStartupProbe", "name", cr.GetName(), "namespace", cr.GetNamespace())

	if startupProbe == nil {
		logger.InfoContext(ctx, "empty startup probe")
		return err
	}

	err = validateProbe(startupProbe)
	if err != nil {
		return fmt.Errorf("invalid Startup Probe config. Reason: %s", err)
	}

	if startupProbe.InitialDelaySeconds != 0 && startupProbe.InitialDelaySeconds < startupProbeDefaultDelaySec {
		logger.InfoContext(ctx, "startup Probe: InitialDelaySeconds is too small, recommended default minimum will be used", "configured", startupProbe.InitialDelaySeconds, "recommendedMinimum", startupProbeDefaultDelaySec)
	}

	if startupProbe.TimeoutSeconds != 0 && startupProbe.TimeoutSeconds < startupProbeTimeoutSec {
		logger.InfoContext(ctx, "startup Probe: TimeoutSeconds is too small, recommended default minimum will be used", "configured", startupProbe.TimeoutSeconds, "recommendedMinimum", startupProbeTimeoutSec)
	}

	if startupProbe.PeriodSeconds != 0 && startupProbe.PeriodSeconds < startupProbePeriodSec {
		logger.InfoContext(ctx, "startup Probe: PeriodSeconds is too small, recommended default minimum will be used", "configured", startupProbe.PeriodSeconds, "recommendedMinimum", startupProbePeriodSec)
	}
	return err
}

func validateSplunkGeneralTerms() error {
	if os.Getenv("SPLUNK_GENERAL_TERMS") == "--accept-sgt-current-at-splunk-com" {
		return nil
	}
	return fmt.Errorf("license not accepted, please adjust SPLUNK_GENERAL_TERMS to indicate you have accepted the current/latest version of the license. See README file for additional information")
}

// configDataHash returns a deterministic SHA-256 hex digest of the ConfigMap
// Data map. Keys are sorted before hashing so the result is stable regardless
// of iteration order. Using the Data hash rather than ResourceVersion prevents
// spurious pod restarts when a ConfigMap is updated (e.g. owner-reference
// annotation during bootstrap) without changing its actual content.
func configDataHash(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%d:%s%d:%s", len(k), k, len(data[k]), data[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}
