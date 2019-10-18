// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package enterprise

import (
	"errors"
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// GetSplunkAppLabels returns a map of labels to use for Splunk instances.
func GetSplunkAppLabels(identifier string, typeLabel string) map[string]string {
	labels := map[string]string{
		"app":  "splunk",
		"for":  identifier,
		"type": typeLabel,
	}

	return labels
}

// AppendSplunkDfsOverrides returns new environment variable overrides that include additional DFS specific variables
func AppendSplunkDfsOverrides(overrides map[string]string, identifier string, searchHeads int) map[string]string {
	// make a copy of original map
	result := make(map[string]string)
	for k, v := range overrides {
		result[k] = v
	}

	// append parameters for DFS
	result["SPLUNK_ENABLE_DFS"] = "true"
	result["SPARK_MASTER_HOST"] = spark.GetSparkServiceName(spark.SPARK_MASTER, identifier)
	result["SPARK_MASTER_WEBUI_PORT"] = "8009"
	result["SPARK_HOME"] = "/mnt/splunk-spark"
	result["JAVA_HOME"] = "/mnt/splunk-jdk"
	if searchHeads > 1 {
		result["SPLUNK_DFW_NUM_SLOTS_ENABLED"] = "true"
	} else {
		result["SPLUNK_DFW_NUM_SLOTS_ENABLED"] = "false"
	}

	return result
}

// GetSplunkConfiguration returns a collection of Kubernetes environment variables (EnvVar) to use for Splunk containers
func GetSplunkConfiguration(overrides map[string]string, defaults string, defaultsUrl string) []corev1.EnvVar {
	splunk_defaults := "/mnt/splunk-secrets/default.yml"
	if defaultsUrl != "" {
		splunk_defaults = fmt.Sprintf("%s,%s", splunk_defaults, defaultsUrl)
	}
	if defaults != "" {
		splunk_defaults = fmt.Sprintf("%s,%s", splunk_defaults, "/mnt/splunk-defaults/default.yml")
	}

	conf := []corev1.EnvVar{
		{
			Name:  "SPLUNK_HOME",
			Value: "/opt/splunk",
		},
		{
			Name:  "SPLUNK_START_ARGS",
			Value: "--accept-license",
		},
		{
			Name:  "SPLUNK_DEFAULTS_URL",
			Value: splunk_defaults,
		},
		{
			Name:  "SPLUNK_HOME_OWNERSHIP_ENFORCEMENT",
			Value: "false",
		},
	}
	for k, v := range overrides {
		conf = append(conf, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}

	return conf
}

// GetSplunkClusterConfiguration returns a collection of Kubernetes environment variables (EnvVar) to use for Splunk containers in clustered deployments
func GetSplunkClusterConfiguration(cr *v1alpha1.SplunkEnterprise, searchHeadCluster bool, overrides map[string]string) []corev1.EnvVar {

	urls := []corev1.EnvVar{
		{
			Name:  "SPLUNK_CLUSTER_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_CLUSTER_MASTER, cr.GetIdentifier()),
		}, {
			Name:  "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SPLUNK_INDEXER, cr.GetIdentifier(), cr.Spec.Topology.Indexers, false),
		}, {
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_LICENSE_MASTER, cr.GetIdentifier()),
		},
	}

	searchHeadConf := []corev1.EnvVar{
		{
			Name:  "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SPLUNK_SEARCH_HEAD, cr.GetIdentifier(), cr.Spec.Topology.SearchHeads, false),
		},
	}

	if searchHeadCluster {
		searchHeadConf = append(searchHeadConf, corev1.EnvVar{
			Name:  "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: GetSplunkStatefulsetUrl(cr.GetNamespace(), SPLUNK_SEARCH_HEAD, cr.GetIdentifier(), 0, false),
		})
		searchHeadConf = append(searchHeadConf, corev1.EnvVar{
			Name:  "SPLUNK_DEPLOYER_URL",
			Value: GetSplunkServiceName(SPLUNK_DEPLOYER, cr.GetIdentifier()),
		})
	}

	return append(append(urls, searchHeadConf...), GetSplunkConfiguration(overrides, cr.Spec.Defaults, cr.Spec.DefaultsUrl)...)
}

// GetSplunkVolumeMounts returns a standard collection of Kubernetes volume claims.
func GetSplunkVolumeClaims(cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType, labels map[string]string) ([]corev1.PersistentVolumeClaim, error) {
	var err error
	var etcStorage, varStorage resource.Quantity

	etcStorage, err = resources.ParseResourceQuantity(cr.Spec.Resources.SplunkEtcStorage, "1Gi")
	if err != nil {
		return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "splunkEtcStorage", err)
	}

	if instanceType == SPLUNK_INDEXER {
		varStorage, err = resources.ParseResourceQuantity(cr.Spec.Resources.SplunkIndexerStorage, "200Gi")
		if err != nil {
			return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "splunkIndexerStorage", err)
		}
	} else {
		varStorage, err = resources.ParseResourceQuantity(cr.Spec.Resources.SplunkVarStorage, "50Gi")
		if err != nil {
			return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "splunkVarStorage", err)
		}
	}

	volumeClaims := []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "etc",
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
				Name:      "var",
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

	if cr.Spec.StorageClassName != "" {
		for idx := range volumeClaims {
			volumeClaims[idx].Spec.StorageClassName = &cr.Spec.StorageClassName
		}
	}

	return volumeClaims, nil
}

// GetSplunkRequirements returns the Kubernetes ResourceRequirements to use for Splunk instances.
func GetSplunkRequirements(cr *v1alpha1.SplunkEnterprise) (corev1.ResourceRequirements, error) {
	cpuRequest, err := resources.ParseResourceQuantity(cr.Spec.Resources.SplunkCpuRequest, "0.1")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SplunkCpuRequest", err)
	}

	memoryRequest, err := resources.ParseResourceQuantity(cr.Spec.Resources.SplunkMemoryRequest, "512Mi")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SplunkMemoryRequest", err)
	}

	cpuLimit, err := resources.ParseResourceQuantity(cr.Spec.Resources.SplunkCpuLimit, "4")
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("%s: %s", "SplunkCpuLimit", err)
	}

	memoryLimit, err := resources.ParseResourceQuantity(cr.Spec.Resources.SplunkMemoryLimit, "8Gi")
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
		}}, nil
}

// GetSplunkStatefulSet returns a Kubernetes StatefulSet object for Splunk instances configured for a SplunkEnterprise resource.
func GetSplunkStatefulSet(cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType, replicas int, envVariables []corev1.EnvVar) (*appsv1.StatefulSet, error) {

	// prepare labels and other values
	labels := GetSplunkAppLabels(cr.GetIdentifier(), instanceType.ToString())
	replicas32 := int32(replicas)
	runAsUser := int64(41812)
	fsGroup := int64(41812)

	// prepare resource requirements
	requirements, err := GetSplunkRequirements(cr)
	if err != nil {
		return nil, err
	}

	// prepare volume claims
	volumeClaims, err := GetSplunkVolumeClaims(cr, instanceType, labels)
	if err != nil {
		return nil, err
	}
	for idx := range volumeClaims {
		volumeClaims[idx].ObjectMeta.Name = fmt.Sprintf("pvc-%s", volumeClaims[idx].ObjectMeta.Name)
	}

	// use script to check if pod is alive
	livenessProbe := corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/sbin/checkstate.sh",
				},
			},
		},
		InitialDelaySeconds:    300,
		TimeoutSeconds:         30,
		PeriodSeconds:          30,
	}

	// configure readiness; note that this also impacts whether or not the pod
	// is available for network communications via headless services. if not
	// "ready", there is no way for other pods to communicate with it.
	readinessProbe := corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/grep",
					"started",
					"/opt/container_artifact/splunk-container.state",
				},
			},
		},
		InitialDelaySeconds:    10,
		TimeoutSeconds:         5,
		PeriodSeconds:          5,
	}

	// create statefulset configuration
	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(instanceType, cr.GetIdentifier()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName:         GetSplunkHeadlessServiceName(instanceType, cr.GetIdentifier()),
			Replicas:            &replicas32,
			PodManagementPolicy: "Parallel",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Affinity:      cr.Spec.Affinity,
					SchedulerName: cr.Spec.SchedulerName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser: &runAsUser,
						FSGroup:   &fsGroup,
					},
					Containers: []corev1.Container{
						{
							Image:           GetSplunkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name:            "splunk",
							Ports:           getSplunkContainerPorts(),
							Env:             envVariables,
							Resources:       requirements,
							VolumeMounts:    getSplunkVolumeMounts(),
							LivenessProbe:   &livenessProbe,
							ReadinessProbe:  &readinessProbe,
						},
					},
				},
			},
			VolumeClaimTemplates: volumeClaims,
		},
	}

	// make SplunkEnterprise object the owner
	statefulSet.SetOwnerReferences(append(statefulSet.GetOwnerReferences(), resources.AsOwner(cr)))

	// update with common splunk pod config
	err = updateSplunkPodTemplateWithConfig(&statefulSet.Spec.Template, cr, instanceType)
	if err != nil {
		return nil, err
	}

	return statefulSet, nil
}

// GetSplunkService returns a Kubernetes Service object for Splunk instances configured for a SplunkEnterprise resource.
func GetSplunkService(cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType, isHeadless bool) *corev1.Service {

	serviceName := GetSplunkServiceName(instanceType, cr.GetIdentifier())
	if isHeadless {
		serviceName = GetSplunkHeadlessServiceName(instanceType, cr.GetIdentifier())
	}

	serviceType := resources.SERVICE
	if isHeadless {
		serviceType = resources.HEADLESS_SERVICE
	}

	serviceTypeLabels := GetSplunkAppLabels(cr.GetIdentifier(), serviceType.ToString())
	selectLabels := GetSplunkAppLabels(cr.GetIdentifier(), instanceType.ToString())

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cr.GetNamespace(),
			Labels:    serviceTypeLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectLabels,
			Ports:    getSplunkServicePorts(),
		},
	}

	if isHeadless {
		service.Spec.ClusterIP = corev1.ClusterIPNone
		// required for SHC bootstrap process; use services with heads when readiness is desired
		service.Spec.PublishNotReadyAddresses = true
	}

	service.SetOwnerReferences(append(service.GetOwnerReferences(), resources.AsOwner(cr)))

	return service
}

// ValidateSplunkCustomResource checks validity of a SplunkEnterprise resource and returns error if something is wrong.
func ValidateSplunkCustomResource(cr *v1alpha1.SplunkEnterprise) error {
	// cluster sanity checks
	if cr.Spec.Topology.SearchHeads > 0 && cr.Spec.Topology.Indexers <= 0 {
		return errors.New("You must specify how many indexers the cluster should have.")
	}
	if cr.Spec.Topology.SearchHeads <= 0 && cr.Spec.Topology.Indexers > 0 {
		return errors.New("You must specify how many search heads the cluster should have.")
	}
	if cr.Spec.Topology.Indexers > 0 && cr.Spec.Topology.SearchHeads > 0 && cr.Spec.LicenseUrl == "" {
		return errors.New("You must provide a license to create a cluster.")
	}

	// default to using a single standalone instance
	if cr.Spec.Topology.SearchHeads <= 0 && cr.Spec.Topology.Indexers <= 0 {
		if cr.Spec.Topology.Standalones <= 0 {
			cr.Spec.Topology.Standalones = 1
		}
	}

	// default to a single spark worker
	if cr.Spec.EnableDFS && cr.Spec.Topology.SparkWorkers <= 0 {
		cr.Spec.Topology.SparkWorkers = 1
	}

	// ImagePullPolicy
	if cr.Spec.ImagePullPolicy == "" {
		cr.Spec.ImagePullPolicy = os.Getenv("IMAGE_PULL_POLICY")
	}
	switch cr.Spec.ImagePullPolicy {
	case "":
		cr.Spec.ImagePullPolicy = "IfNotPresent"
		break
	case "Always":
		break
	case "IfNotPresent":
		break
	default:
		return fmt.Errorf("ImagePullPolicy must be one of \"Always\" or \"IfNotPresent\"; value=\"%s\"",
			cr.Spec.ImagePullPolicy)
	}

	// SchedulerName
	if cr.Spec.SchedulerName == "" {
		cr.Spec.SchedulerName = "default-scheduler"
	}

	return nil
}

// GetSplunkDefaults returns a Kubernetes ConfigMap containing defaults for a SplunkEnterprise resource.
func GetSplunkDefaults(cr *v1alpha1.SplunkEnterprise) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkDefaultsName(cr.GetIdentifier()),
			Namespace: cr.GetNamespace(),
		},
		Data: map[string]string{
			"default.yml": cr.Spec.Defaults,
		},
	}
}

// GetSplunkSecrets returns a Kubernetes Secret containing randomly generated default secrets to use for a SplunkEnterprise resource.
func GetSplunkSecrets(cr *v1alpha1.SplunkEnterprise) *corev1.Secret {
	// generate some default secret values to share across the cluster
	secretData := map[string][]byte{
		"password":   resources.GenerateSecret(SPLUNK_SECRET_LEN),
		"hec_token":  resources.GenerateSecret(SPLUNK_SECRET_LEN),
		"idc_secret": resources.GenerateSecret(SPLUNK_SECRET_LEN),
		"shc_secret": resources.GenerateSecret(SPLUNK_SECRET_LEN),
	}
	secretData["default.yml"] = []byte(fmt.Sprintf(`
splunk:
    password: "%s"
    hec_token: "%s"
    idc:
        secret: "%s"
    shc:
        secret: "%s"
`,
		secretData["password"],
		secretData["hec_token"],
		secretData["idc_secret"],
		secretData["shc_secret"]))

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkSecretsName(cr.GetIdentifier()),
			Namespace: cr.GetNamespace(),
		},
		Data: secretData,
	}
}

// getSplunkPorts returns a map of ports to use for Splunk instances.
func getSplunkPorts() map[string]int {
	return map[string]int{
		"splunkweb":   8000,
		"splunkd":     8089,
		"dfccontrol":  17000,
		"datarecieve": 19000,
		"dfsmaster":   9000,
	}
}

// getSplunkContainerPorts returns a list of Kubernetes ContainerPort objects for Splunk instances.
func getSplunkContainerPorts() []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range getSplunkPorts() {
		l = append(l, corev1.ContainerPort{
			Name:          key,
			ContainerPort: int32(value),
			Protocol:      "TCP",
		})
	}
	return l
}

// getSplunkServicePorts returns a list of Kubernetes ServicePort objects for Splunk instances.
func getSplunkServicePorts() []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range getSplunkPorts() {
		l = append(l, corev1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}

// getSplunkVolumeMounts returns a standard collection of Kubernetes volume mounts.
func getSplunkVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
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

// addSplunkVolumeToTemplate modifies the podTemplateSpec object to incorporates an additional VolumeSource.
func addSplunkVolumeToTemplate(podTemplateSpec *corev1.PodTemplateSpec, name string, volumeSource corev1.VolumeSource) {
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, corev1.Volume{
		Name:         "mnt-splunk-" + name,
		VolumeSource: volumeSource,
	})

	for idx := range podTemplateSpec.Spec.Containers {
		containerSpec := &podTemplateSpec.Spec.Containers[idx]
		containerSpec.VolumeMounts = append(containerSpec.VolumeMounts, corev1.VolumeMount{
			Name:      "mnt-splunk-" + name,
			MountPath: "/mnt/splunk-" + name,
		})
	}
}

// addDFCToPodTemplate modifies the podTemplateSpec object to incorporate support for DFS.
func addDFCToPodTemplate(podTemplateSpec *corev1.PodTemplateSpec, cr *v1alpha1.SplunkEnterprise) error {
	requirements, err := spark.GetSparkRequirements(cr)
	if err != nil {
		return err
	}

	// create an init container in the pod, which is just used to populate the jdk and spark mount directories
	containerSpec := corev1.Container{
		Image:           spark.GetSparkImage(cr),
		ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
		Name:            "init",
		Resources:       requirements,
		Command:         []string{"bash", "-c", "cp -r /opt/jdk /mnt && cp -r /opt/spark /mnt"},
	}
	containerSpec.VolumeMounts = append(containerSpec.VolumeMounts, corev1.VolumeMount{
		Name:      "mnt-splunk-jdk",
		MountPath: "/mnt/jdk",
	})
	containerSpec.VolumeMounts = append(containerSpec.VolumeMounts, corev1.VolumeMount{
		Name:      "mnt-splunk-spark",
		MountPath: "/mnt/spark",
	})
	podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, containerSpec)

	// add empty jdk and spark mount directories to all of the splunk containers
	emptyVolumeSource := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}
	addSplunkVolumeToTemplate(podTemplateSpec, "jdk", emptyVolumeSource)
	addSplunkVolumeToTemplate(podTemplateSpec, "spark", emptyVolumeSource)

	return nil
}

// updateSplunkPodTemplateWithConfig modifies the podTemplateSpec object based on configuraton of the SplunkEnterprise resource.
func updateSplunkPodTemplateWithConfig(podTemplateSpec *corev1.PodTemplateSpec, cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType) error {

	// Add custom volumes to splunk containers
	if cr.Spec.SplunkVolumes != nil {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, cr.Spec.SplunkVolumes...)
		for idx := range podTemplateSpec.Spec.Containers {
			for v := range cr.Spec.SplunkVolumes {
				podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(podTemplateSpec.Spec.Containers[idx].VolumeMounts, corev1.VolumeMount{
					Name:      cr.Spec.SplunkVolumes[v].Name,
					MountPath: "/mnt/" + cr.Spec.SplunkVolumes[v].Name,
				})
			}
		}
	}

	// add defaults secrets to all splunk containers
	addSplunkVolumeToTemplate(podTemplateSpec, "secrets", corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName: GetSplunkSecretsName(cr.GetIdentifier()),
		},
	})

	// add inline defaults to all splunk containers
	if cr.Spec.Defaults != "" {
		addSplunkVolumeToTemplate(podTemplateSpec, "defaults", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: GetSplunkDefaultsName(cr.GetIdentifier()),
				},
			},
		})
	}

	// add spark and java mounts to search head containers
	if cr.Spec.EnableDFS && (instanceType == SPLUNK_SEARCH_HEAD || instanceType == SPLUNK_STANDALONE) {
		err := addDFCToPodTemplate(podTemplateSpec, cr)
		if err != nil {
			return err
		}
	}

	return nil
}
