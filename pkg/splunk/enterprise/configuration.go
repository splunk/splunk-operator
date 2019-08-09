package enterprise

import (
	"math/rand"
	"time"
	"fmt"
	"os"
	"errors"

	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/resources"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	SPLUNK_SECRET_BYTES = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890%()*+,-./<=>?@[]^_{|}~"
	SPLUNK_SECRET_LEN = 24
)

func init() {
	// seed rng for splunk secret generation
	rand.Seed(time.Now().UnixNano())
}


func GetSplunkAppLabels(identifier string, typeLabel string) map[string]string {
	labels := map[string]string{
		"app": "splunk",
		"for": identifier,
		"type": typeLabel,
	}

	return labels
}


func GetSplunkPorts() map[string]int {
	return map[string]int{
		"splunkweb": 8000,
		"splunkd": 8089,
		"dfccontrol": 17000,
		"datarecieve": 19000,
		"dfsmaster": 9000,
	}
}


func GetSplunkContainerPorts() []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range GetSplunkPorts() {
		l = append(l, corev1.ContainerPort{
			Name: key,
			ContainerPort: int32(value),
		})
	}
	return l
}


func GetSplunkServicePorts() []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range GetSplunkPorts() {
		l = append(l, corev1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}


func AppendSplunkDfsOverrides(cr *v1alpha1.SplunkEnterprise, overrides map[string]string) {
	// assertion: overrides is not nil
	if cr.Spec.EnableDFS {
		overrides["SPLUNK_ENABLE_DFS"] = "true"
		overrides["SPARK_MASTER_HOST"] = spark.GetSparkServiceName(spark.SPARK_MASTER, GetIdentifier(cr))
		overrides["SPARK_MASTER_WEBUI_PORT"] = "8009"
		overrides["SPARK_HOME"] = "/mnt/splunk-spark"
		overrides["JAVA_HOME"] = "/mnt/splunk-jdk"
		if cr.Spec.Topology.SearchHeads > 1 {
			overrides["SPLUNK_DFW_NUM_SLOTS_ENABLED"] = "true"
		} else {
			overrides["SPLUNK_DFW_NUM_SLOTS_ENABLED"] = "false"
		}
	}
}


func GetSplunkConfiguration(cr *v1alpha1.SplunkEnterprise, overrides map[string]string) []corev1.EnvVar {
	splunk_defaults := "/mnt/splunk-secrets/default.yml"
	if cr.Spec.DefaultsUrl != "" {
		splunk_defaults = fmt.Sprintf("%s,%s", splunk_defaults, cr.Spec.DefaultsUrl)
	}
	if cr.Spec.Defaults != "" {
		splunk_defaults = fmt.Sprintf("%s,%s", splunk_defaults, "/mnt/splunk-defaults/default.yml")
	}

	conf := []corev1.EnvVar{
		{
			Name: "SPLUNK_HOME",
			Value: "/opt/splunk",
		},
		{
			Name: "SPLUNK_START_ARGS",
			Value: "--accept-license",
		},
		{
			Name: "SPLUNK_DEFAULTS_URL",
			Value: splunk_defaults,
		},
	}

	if overrides != nil {
		for k, v := range overrides {
			conf = append(conf, corev1.EnvVar{
				Name: k,
				Value: v,
			})
		}
	}

	return conf
}


func GetSplunkClusterConfiguration(cr *v1alpha1.SplunkEnterprise, searchHeadCluster bool, overrides map[string]string) []corev1.EnvVar {
	identifier := GetIdentifier(cr)
	namespace := GetNamespace(cr)

	urls := []corev1.EnvVar{
		{
			Name: "SPLUNK_CLUSTER_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_CLUSTER_MASTER, identifier),
		},{
			Name: "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(namespace, SPLUNK_INDEXER, identifier, cr.Spec.Topology.Indexers, false),
		},{
			Name: "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_LICENSE_MASTER, identifier),
		},
	}

	searchHeadConf := []corev1.EnvVar{
		{
			Name: "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(namespace, SPLUNK_SEARCH_HEAD, identifier, cr.Spec.Topology.SearchHeads, false),
		},
	}

	if searchHeadCluster {
		searchHeadConf = append(searchHeadConf, corev1.EnvVar{
			Name: "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: GetSplunkStatefulsetUrl(namespace, SPLUNK_SEARCH_HEAD, identifier, 0, false),
		})
		searchHeadConf = append(searchHeadConf, corev1.EnvVar{
			Name: "SPLUNK_DEPLOYER_URL",
			Value: GetSplunkServiceName(SPLUNK_DEPLOYER, identifier),
		})
	}

	return append(append(urls, searchHeadConf...), GetSplunkConfiguration(cr, overrides)...)
}


func AddSplunkVolumeToTemplate(podTemplateSpec *corev1.PodTemplateSpec, name string, volumeSource corev1.VolumeSource) {
	podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, corev1.Volume{
		Name: "mnt-splunk-" + name,
		VolumeSource: volumeSource,
	})

	for idx, _ := range podTemplateSpec.Spec.Containers {
		containerSpec := &podTemplateSpec.Spec.Containers[idx]
		containerSpec.VolumeMounts = append(containerSpec.VolumeMounts, corev1.VolumeMount{
			Name:      "mnt-splunk-" + name,
			MountPath: "/mnt/splunk-" + name,
		})
	}
}


func AddDFCToPodTemplate(podTemplateSpec *corev1.PodTemplateSpec, instance *v1alpha1.SplunkEnterprise) error {
	requirements, err := spark.GetSparkRequirements(instance)
	if err != nil {
		return err
	}

	// create an init container in the pod, which is just used to populate the jdk and spark mount directories
	containerSpec := corev1.Container {
		Image: spark.GetSparkImage(instance),
		ImagePullPolicy: corev1.PullPolicy(instance.Spec.ImagePullPolicy),
		Name: "init",
		Resources: requirements,
		Command: []string{ "bash", "-c", "cp -r /opt/jdk /mnt && cp -r /opt/spark /mnt" },
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
	AddSplunkVolumeToTemplate(podTemplateSpec, "jdk", emptyVolumeSource)
	AddSplunkVolumeToTemplate(podTemplateSpec, "spark", emptyVolumeSource)

	return nil
}


func UpdateSplunkPodTemplateWithConfig(podTemplateSpec *corev1.PodTemplateSpec, cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType) error {

	// Add custom volumes to splunk containers
	if cr.Spec.SplunkVolumes != nil {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, cr.Spec.SplunkVolumes...)
		for idx, _ := range podTemplateSpec.Spec.Containers {
			for v, _ := range cr.Spec.SplunkVolumes {
				podTemplateSpec.Spec.Containers[idx].VolumeMounts = append(podTemplateSpec.Spec.Containers[idx].VolumeMounts, corev1.VolumeMount{
					Name:      cr.Spec.SplunkVolumes[v].Name,
					MountPath: "/mnt/" + cr.Spec.SplunkVolumes[v].Name,
				})
			}
		}
	}

	// add defaults secrets to all splunk containers
	AddSplunkVolumeToTemplate(podTemplateSpec, "secrets", corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName: GetSplunkSecretsName(GetIdentifier(cr)),
		},
	})

	// add inline defaults to all splunk containers
	if cr.Spec.Defaults != "" {
		AddSplunkVolumeToTemplate(podTemplateSpec, "defaults", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: GetSplunkDefaultsName(GetIdentifier(cr)),
				},
			},
		})
	}

	// add spark and java mounts to search head containers
	if cr.Spec.EnableDFS && (instanceType == SPLUNK_SEARCH_HEAD || instanceType == SPLUNK_STANDALONE) {
		if err := AddDFCToPodTemplate(podTemplateSpec, cr); err != nil {
			return err
		}
	}

	return resources.UpdatePodTemplateWithConfig(podTemplateSpec, cr)
}


func GetSplunkVolumeMounts() ([]corev1.VolumeMount) {
	return []corev1.VolumeMount{
		corev1.VolumeMount{
			Name:      "pvc-etc",
			MountPath: "/opt/splunk/etc",
		},
		corev1.VolumeMount{
			Name:      "pvc-var",
			MountPath: "/opt/splunk/var",
		},
	}
}


func GetSplunkVolumeClaims(cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType, labels map[string]string) ([]corev1.PersistentVolumeClaim, error) {
	var err error
	var etcStorage, varStorage resource.Quantity

	etcStorage, err = resources.ParseResourceQuantity(cr.Spec.Resources.SplunkEtcStorage, "1Gi")
	if err != nil {
		return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "splunkEtcStorage", err)
	}

	if (instanceType == SPLUNK_INDEXER) {
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
		corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "etc",
				Namespace: cr.Namespace,
				Labels: labels,
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
		corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "var",
				Namespace: cr.Namespace,
				Labels: labels,
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

	if (cr.Spec.StorageClassName != "") {
		for idx, _ := range volumeClaims {
			volumeClaims[idx].Spec.StorageClassName = &cr.Spec.StorageClassName
		}
	}

	return volumeClaims, nil
}


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
		} }, nil
}


func GetSplunkDeployment(cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType, deploymentName string, labels map[string]string, replicas int, envVariables []corev1.EnvVar) (*appsv1.Deployment, error) {

	replicas32 := int32(replicas)

	requirements, err := GetSplunkRequirements(cr)
	if err != nil {
		return nil, err
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentName,
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
					Containers: []corev1.Container{
						{
							Image: GetSplunkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name: "splunk",
							Ports: GetSplunkContainerPorts(),
							Env: envVariables,
							Resources: requirements,
							VolumeMounts: GetSplunkVolumeMounts(),
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "pvc-etc",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "pvc-etc-" + deploymentName,
								},
							},
						},
						{
							Name: "pvc-var",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "pvc-var-" + deploymentName,
								},
							},
						},
					},
				},
			},
		},
	}

	resources.AddOwnerRefToObject(deployment, resources.AsOwner(cr))

	err = UpdateSplunkPodTemplateWithConfig(&deployment.Spec.Template, cr, instanceType)
	if err != nil {
		return nil, err
	}

	return deployment, nil
}


func GetSplunkStatefulSet(cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar) (*appsv1.StatefulSet, error) {

	labels := GetSplunkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	requirements, err := GetSplunkRequirements(cr)
	if err != nil {
		return nil, err
	}

	volumeClaims, err := GetSplunkVolumeClaims(cr, instanceType, labels)
	if err != nil {
		return nil, err
	}
	for idx, _ := range volumeClaims {
		volumeClaims[idx].ObjectMeta.Name = fmt.Sprintf("pvc-%s", volumeClaims[idx].ObjectMeta.Name)
	}

	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: GetSplunkStatefulsetName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: GetSplunkHeadlessServiceName(instanceType, identifier),
			Replicas: &replicas32,
			PodManagementPolicy: "Parallel",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: GetSplunkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name: "splunk",
							Ports: GetSplunkContainerPorts(),
							Env: envVariables,
							Resources: requirements,
							VolumeMounts: GetSplunkVolumeMounts(),
						},
					},
				},
			},
			VolumeClaimTemplates: volumeClaims,
		},
	}

	resources.AddOwnerRefToObject(statefulSet, resources.AsOwner(cr))

	err = UpdateSplunkPodTemplateWithConfig(&statefulSet.Spec.Template, cr, instanceType)
	if err != nil {
		return nil, err
	}

	return statefulSet, nil
}


func GetSplunkService(cr *v1alpha1.SplunkEnterprise, instanceType SplunkInstanceType, identifier string, isHeadless bool) *corev1.Service {

	serviceName := GetSplunkServiceName(instanceType, identifier)
	if isHeadless {
		serviceName = GetSplunkHeadlessServiceName(instanceType, identifier)
	}

	serviceType := resources.SERVICE
	if isHeadless {
		serviceType = resources.HEADLESS_SERVICE
	}

	serviceTypeLabels := GetSplunkAppLabels(identifier, serviceType.ToString())
	selectLabels := GetSplunkAppLabels(identifier, instanceType.ToString())

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
			Namespace: cr.Namespace,
			Labels: serviceTypeLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectLabels,
			Ports: GetSplunkServicePorts(),
		},
	}

	if isHeadless {
		service.Spec.ClusterIP = corev1.ClusterIPNone
	}

	resources.AddOwnerRefToObject(service, resources.AsOwner(cr))

	return service
}


func ValidateSplunkCustomResource(instance *v1alpha1.SplunkEnterprise) error {
	// cluster sanity checks
	if instance.Spec.Topology.SearchHeads > 0 && instance.Spec.Topology.Indexers <= 0 {
		return errors.New("You must specify how many indexers the cluster should have.")
	}
	if instance.Spec.Topology.SearchHeads <= 0 && instance.Spec.Topology.Indexers > 0 {
		return errors.New("You must specify how many search heads the cluster should have.")
	}
	if instance.Spec.Topology.Indexers > 0 && instance.Spec.Topology.SearchHeads > 0 && instance.Spec.LicenseUrl == "" {
		return errors.New("You must provide a license to create a cluster.")
	}

	// default to using a single standalone instance
	if instance.Spec.Topology.SearchHeads <= 0 && instance.Spec.Topology.Indexers <= 0 {
		if instance.Spec.Topology.Standalones <= 0 {
			instance.Spec.Topology.Standalones = 1
		}
	}

	// default to a single spark worker
	if instance.Spec.EnableDFS && instance.Spec.Topology.SparkWorkers <= 0 {
		instance.Spec.Topology.SparkWorkers = 1
	}

	// ImagePullPolicy
	if (instance.Spec.ImagePullPolicy == "") {
		instance.Spec.ImagePullPolicy = os.Getenv("IMAGE_PULL_POLICY")
	}
	switch (instance.Spec.ImagePullPolicy) {
	case "":
		instance.Spec.ImagePullPolicy = "IfNotPresent"
		break
	case "Always":
		break
	case "IfNotPresent":
		break
	default:
		return fmt.Errorf("ImagePullPolicy must be one of \"Always\" or \"IfNotPresent\"; value=\"%s\"",
			instance.Spec.ImagePullPolicy)
	}

	return nil
}


func GetSplunkDefaults(cr *v1alpha1.SplunkEnterprise) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetSplunkDefaultsName(GetIdentifier(cr)),
			Namespace: GetNamespace(cr),
		},
		Data: map[string]string{
			"default.yml" : cr.Spec.Defaults,
		},
	}
}


func GetSplunkSecrets(cr *v1alpha1.SplunkEnterprise) *corev1.Secret {
	// generate some default secret values to share across the cluster
	secretData := map[string][]byte {
		"password" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
		"hec_token" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
		"idc_secret" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
		"shc_secret" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
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
			Name: GetSplunkSecretsName(GetIdentifier(cr)),
			Namespace: GetNamespace(cr),
		},
		Data: secretData,
	}
}


func GenerateSplunkSecret(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = SPLUNK_SECRET_BYTES[rand.Int63() % int64(len(SPLUNK_SECRET_BYTES))]
	}
	return b
}
