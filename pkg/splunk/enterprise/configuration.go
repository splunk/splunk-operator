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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// getSplunkLabels returns a map of labels to use for Splunk Enterprise components.
func getSplunkLabels(identifier string, instanceType InstanceType) map[string]string {
	return resources.GetLabels(instanceType.ToKind(), instanceType.ToString(), identifier)
}

// getSplunkVolumeClaims returns a standard collection of Kubernetes volume claims.
func getSplunkVolumeClaims(cr enterprisev1.MetaObject, spec *enterprisev1.CommonSplunkSpec, labels map[string]string) ([]corev1.PersistentVolumeClaim, error) {
	var etcStorage, varStorage resource.Quantity
	var err error

	etcStorage, err = resources.ParseResourceQuantity(spec.EtcStorage, "10Gi")
	if err != nil {
		return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "etcStorage", err)
	}

	varStorage, err = resources.ParseResourceQuantity(spec.VarStorage, "100Gi")
	if err != nil {
		return []corev1.PersistentVolumeClaim{}, fmt.Errorf("%s: %s", "varStorage", err)
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

	if spec.StorageClassName != "" {
		for idx := range volumeClaims {
			volumeClaims[idx].Spec.StorageClassName = &spec.StorageClassName
		}
	}

	return volumeClaims, nil
}

// GetStandaloneStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise standalone instances.
func GetStandaloneStatefulSet(cr *enterprisev1.Standalone) (*appsv1.StatefulSet, error) {

	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, cr.Spec.Replicas, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}

	// add spark and java mounts to search head containers
	if cr.Spec.SparkRef.Name != "" {
		addDFCToPodTemplate(&ss.Spec.Template, cr.Spec.SparkRef, cr.Spec.SparkImage, cr.Spec.ImagePullPolicy, cr.Spec.Replicas > 1)
	}

	return ss, nil
}

// GetSearchHeadStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise search heads.
func GetSearchHeadStatefulSet(cr *enterprisev1.SearchHeadCluster) (*appsv1.StatefulSet, error) {

	// get search head env variables with deployer
	env := getSearchHeadExtraEnv(cr, cr.Spec.Replicas)
	env = append(env, corev1.EnvVar{
		Name:  "SPLUNK_DEPLOYER_URL",
		Value: GetSplunkServiceName(SplunkDeployer, cr.GetIdentifier(), false),
	})

	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(cr, &cr.Spec.CommonSplunkSpec, SplunkSearchHead, cr.Spec.Replicas, env)
	if err != nil {
		return nil, err
	}

	// add spark and java mounts to search head containers
	if cr.Spec.SparkRef.Name != "" {
		addDFCToPodTemplate(&ss.Spec.Template, cr.Spec.SparkRef, cr.Spec.SparkImage, cr.Spec.ImagePullPolicy, cr.Spec.Replicas > 1)
	}

	return ss, nil
}

// GetIndexerStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise indexers.
func GetIndexerStatefulSet(cr *enterprisev1.IndexerCluster) (*appsv1.StatefulSet, error) {
	return getSplunkStatefulSet(cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, cr.Spec.Replicas, getIndexerExtraEnv(cr, cr.Spec.Replicas))
}

// GetClusterMasterStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func GetClusterMasterStatefulSet(cr *enterprisev1.IndexerCluster) (*appsv1.StatefulSet, error) {
	return getSplunkStatefulSet(cr, &cr.Spec.CommonSplunkSpec, SplunkClusterMaster, 1, getIndexerExtraEnv(cr, cr.Spec.Replicas))
}

// GetDeployerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func GetDeployerStatefulSet(cr *enterprisev1.SearchHeadCluster) (*appsv1.StatefulSet, error) {
	return getSplunkStatefulSet(cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, 1, getSearchHeadExtraEnv(cr, cr.Spec.Replicas))
}

// GetLicenseMasterStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func GetLicenseMasterStatefulSet(cr *enterprisev1.LicenseMaster) (*appsv1.StatefulSet, error) {
	return getSplunkStatefulSet(cr, &cr.Spec.CommonSplunkSpec, SplunkLicenseMaster, 1, []corev1.EnvVar{})
}

// GetSplunkService returns a Kubernetes Service object for Splunk instances configured for a Splunk Enterprise resource.
func GetSplunkService(cr enterprisev1.MetaObject, spec enterprisev1.CommonSpec, instanceType InstanceType, isHeadless bool) *corev1.Service {

	// use template if not headless
	var service *corev1.Service
	if isHeadless {
		service = &corev1.Service{}
		service.Spec.ClusterIP = corev1.ClusterIPNone
	} else {
		service = spec.ServiceTemplate.DeepCopy()
	}
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}
	service.ObjectMeta.Name = GetSplunkServiceName(instanceType, cr.GetIdentifier(), isHeadless)
	service.ObjectMeta.Namespace = cr.GetNamespace()
	service.Spec.Selector = getSplunkLabels(cr.GetIdentifier(), instanceType)
	service.Spec.Ports = resources.SortServicePorts(getSplunkServicePorts(instanceType)) // note that port order is important for tests

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
	resources.AppendParentMeta(service.ObjectMeta.GetObjectMeta(), cr.GetObjectMeta())

	if instanceType == SplunkDeployer || (instanceType == SplunkSearchHead && isHeadless) {
		// required for SHC bootstrap process; use services with heads when readiness is desired
		service.Spec.PublishNotReadyAddresses = true
	}

	service.SetOwnerReferences(append(service.GetOwnerReferences(), resources.AsOwner(cr)))

	return service
}

// validateCommonSplunkSpec checks validity and makes default updates to a CommonSplunkSpec, and returns error if something is wrong.
func validateCommonSplunkSpec(spec *enterprisev1.CommonSplunkSpec) error {
	// if not specified via spec or env, image defaults to splunk/splunk
	spec.CommonSpec.Image = GetSplunkImage(spec.CommonSpec.Image)
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
	// work-around openapi validation error by ensuring it is not nill
	if spec.Volumes == nil {
		spec.Volumes = []corev1.Volume{}
	}
	return resources.ValidateCommonSpec(&spec.CommonSpec, defaultResources)
}

// ValidateIndexerClusterSpec checks validity and makes default updates to a IndexerClusterSpec, and returns error if something is wrong.
func ValidateIndexerClusterSpec(spec *enterprisev1.IndexerClusterSpec) error {
	if spec.Replicas == 0 {
		spec.Replicas = 1
	}
	return validateCommonSplunkSpec(&spec.CommonSplunkSpec)
}

// ValidateSearchHeadClusterSpec checks validity and makes default updates to a SearchHeadClusterSpec, and returns error if something is wrong.
func ValidateSearchHeadClusterSpec(spec *enterprisev1.SearchHeadClusterSpec) error {
	if spec.Replicas < 3 {
		spec.Replicas = 3
	}
	spec.SparkImage = spark.GetSparkImage(spec.SparkImage)
	return validateCommonSplunkSpec(&spec.CommonSplunkSpec)
}

// ValidateStandaloneSpec checks validity and makes default updates to a StandaloneSpec, and returns error if something is wrong.
func ValidateStandaloneSpec(spec *enterprisev1.StandaloneSpec) error {
	if spec.Replicas == 0 {
		spec.Replicas = 1
	}
	spec.SparkImage = spark.GetSparkImage(spec.SparkImage)
	return validateCommonSplunkSpec(&spec.CommonSplunkSpec)
}

// ValidateLicenseMasterSpec checks validity and makes default updates to a LicenseMasterSpec, and returns error if something is wrong.
func ValidateLicenseMasterSpec(spec *enterprisev1.LicenseMasterSpec) error {
	return validateCommonSplunkSpec(&spec.CommonSplunkSpec)
}

// UpdateSearchHeadClusterStatus uses the REST API to update the status for a SearcHead custom resource
func UpdateSearchHeadClusterStatus(cr *enterprisev1.SearchHeadCluster, secrets *corev1.Secret) error {
	username := "admin"
	password := string(secrets.Data["password"])

	// populate members status using REST API to get search head cluster member info
	cr.Status.Members = []enterprisev1.SearchHeadClusterMemberStatus{}
	for n := int32(0); n < cr.Spec.Replicas; n++ {
		memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, cr.GetIdentifier(), n)
		fqdnName := resources.GetServiceFQDN(cr.GetNamespace(),
			fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkSearchHead, cr.GetIdentifier(), true)))
		c := NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), username, password)
		memberStatus := enterprisev1.SearchHeadClusterMemberStatus{Name: memberName}
		memberInfo, err := c.GetSearchHeadClusterMemberInfo()
		if err == nil {
			memberStatus.Status = memberInfo.Status
			memberStatus.Registered = memberInfo.Registered
			memberStatus.ActiveSearches = memberInfo.ActiveHistoricalSearchCount + memberInfo.ActiveRealtimeSearchCount
		}
		cr.Status.Members = append(cr.Status.Members, memberStatus)
	}

	// get search head cluster info from captain
	fqdnName := resources.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkSearchHead, cr.GetIdentifier(), false))
	c := NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), username, password)
	captainInfo, err := c.GetSearchHeadCaptainInfo()
	if err != nil {
		return err
	}
	cr.Status.Captain = captainInfo.Label
	cr.Status.CaptainReady = captainInfo.ServiceReadyFlag
	cr.Status.Initialized = captainInfo.InitializedFlag
	cr.Status.MinPeersJoined = captainInfo.MinPeersJoinedFlag
	cr.Status.MaintenanceMode = captainInfo.MaintenanceMode

	return nil
}

// DecommissionSearchHead detains and then removes a search head from the cluster
func DecommissionSearchHead(cr *enterprisev1.SearchHeadCluster, secrets *corev1.Secret, n int32) (bool, error) {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, cr.GetIdentifier(), n)
	fqdnName := resources.GetServiceFQDN(cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkSearchHead, cr.GetIdentifier(), true)))
	c := NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(secrets.Data["password"]))

	switch cr.Status.Members[n].Status {
	case "Up":
		// Detain search head
		return false, c.SetSearchHeadDetention(true)

	case "ManualDetention":
		// Wait until active searches have drained
		if cr.Status.Members[n].ActiveSearches != 0 {
			return false, c.RemoveSearchHeadClusterMember()
		}
	}

	// completed
	return true, nil
}

// GetSplunkDefaults returns a Kubernetes ConfigMap containing defaults for a Splunk Enterprise resource.
func GetSplunkDefaults(identifier, namespace string, instanceType InstanceType, defaults string) *corev1.ConfigMap {
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

// GetSplunkSecrets returns a Kubernetes Secret containing randomly generated default secrets to use for a Splunk Enterprise resource.
func GetSplunkSecrets(cr enterprisev1.MetaObject, instanceType InstanceType, idxcSecret []byte, pass4SymmKey []byte) *corev1.Secret {
	// idxc_secret is option, and may be used to override random generation
	if len(idxcSecret) == 0 {
		idxcSecret = generateSplunkSecret()
	}

	// pass4SymmKey is option, and may be used to override random generation
	if len(pass4SymmKey) == 0 {
		pass4SymmKey = generateSplunkSecret()
	}

	// generate some default secret values to share across the cluster
	secretData := map[string][]byte{
		"hec_token":    generateHECToken(),
		"password":     generateSplunkSecret(),
		"pass4SymmKey": pass4SymmKey,
		"idxc_secret":  idxcSecret,
		"shc_secret":   generateSplunkSecret(),
	}
	secretData["default.yml"] = []byte(fmt.Sprintf(`
splunk:
    hec_disabled: 0
    hec_enableSSL: 0
    hec_token: "%s"
    password: "%s"
    pass4SymmKey: "%s"
    idxc:
        secret: "%s"
    shc:
        secret: "%s"
`,
		secretData["hec_token"],
		secretData["password"],
		secretData["pass4SymmKey"],
		secretData["idxc_secret"],
		secretData["shc_secret"]))

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkSecretsName(cr.GetIdentifier(), instanceType),
			Namespace: cr.GetNamespace(),
		},
		Data: secretData,
	}
}

// generateSplunkSecret returns a randomly generated Splunk secret.
func generateSplunkSecret() []byte {
	return resources.GenerateSecret(secretBytes, 24)
}

// generateHECToken returns a randomly generated HEC token formatted like a UUID.
// Note that it is not strictly a UUID, but rather just looks like one.
func generateHECToken() []byte {
	hecToken := resources.GenerateSecret(hexBytes, 36)
	hecToken[8] = '-'
	hecToken[13] = '-'
	hecToken[18] = '-'
	hecToken[23] = '-'
	return hecToken
}

// getSplunkPorts returns a map of ports to use for Splunk instances.
func getSplunkPorts(instanceType InstanceType) map[string]int {
	result := map[string]int{
		"splunkweb": 8000,
		"splunkd":   8089,
	}

	switch instanceType {
	case SplunkStandalone:
		result["dfccontrol"] = 17000
		result["datarecieve"] = 19000
		result["dfsmaster"] = 9000
		result["hec"] = 8088
		result["s2s"] = 9997
	case SplunkSearchHead:
		result["dfccontrol"] = 17000
		result["datarecieve"] = 19000
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
			Protocol:      "TCP",
		})
	}
	return l
}

// getSplunkServicePorts returns a list of Kubernetes ServicePort objects for Splunk instances.
func getSplunkServicePorts(instanceType InstanceType) []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range getSplunkPorts(instanceType) {
		l = append(l, corev1.ServicePort{
			Name:     key,
			Port:     int32(value),
			Protocol: "TCP",
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
func addDFCToPodTemplate(podTemplateSpec *corev1.PodTemplateSpec, sparkRef corev1.ObjectReference, sparkImage string, imagePullPolicy string, slotsEnabled bool) {
	// create an init container in the pod, which is just used to populate the jdk and spark mount directories
	containerSpec := corev1.Container{
		Image:           sparkImage,
		ImagePullPolicy: corev1.PullPolicy(imagePullPolicy),
		Name:            "init",
		Command:         []string{"bash", "-c", "cp -r /opt/jdk /mnt && cp -r /opt/spark /mnt"},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "mnt-splunk-jdk", MountPath: "/mnt/jdk"},
			{Name: "mnt-splunk-spark", MountPath: "/mnt/spark"},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.25"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}
	podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, containerSpec)

	// add empty jdk and spark mount directories to all of the splunk containers
	emptyVolumeSource := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}
	addSplunkVolumeToTemplate(podTemplateSpec, "jdk", emptyVolumeSource)
	addSplunkVolumeToTemplate(podTemplateSpec, "spark", emptyVolumeSource)

	// prepare spark master host URL
	sparkMasterHost := spark.GetSparkServiceName(spark.SparkMaster, sparkRef.Name, false)
	if sparkRef.Namespace != "" {
		sparkMasterHost = resources.GetServiceFQDN(sparkRef.Namespace, sparkMasterHost)
	}

	// append DFS env variables to splunk enterprise containers
	dfsEnvVar := []corev1.EnvVar{
		{Name: "SPLUNK_ENABLE_DFS", Value: "true"},
		{Name: "SPARK_MASTER_HOST", Value: sparkMasterHost},
		{Name: "SPARK_MASTER_WEBUI_PORT", Value: "8009"},
		{Name: "SPARK_HOME", Value: "/mnt/splunk-spark"},
		{Name: "JAVA_HOME", Value: "/mnt/splunk-jdk"},
		{Name: "SPLUNK_DFW_NUM_SLOTS_ENABLED", Value: "true"},
	}
	if !slotsEnabled {
		dfsEnvVar[5].Value = "false"
	}
	for idx := range podTemplateSpec.Spec.Containers {
		podTemplateSpec.Spec.Containers[idx].Env = append(podTemplateSpec.Spec.Containers[idx].Env, dfsEnvVar...)
	}
}

// getSplunkStatefulSet returns a Kubernetes StatefulSet object for Splunk instances configured for a Splunk Enterprise resource.
func getSplunkStatefulSet(cr enterprisev1.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, replicas int32, extraEnv []corev1.EnvVar) (*appsv1.StatefulSet, error) {

	// prepare misc values
	ports := resources.SortContainerPorts(getSplunkContainerPorts(instanceType)) // note that port order is important for tests
	annotations := resources.GetIstioAnnotations(ports)
	selectLabels := getSplunkLabels(cr.GetIdentifier(), instanceType)
	affinity := resources.AppendPodAntiAffinity(&spec.Affinity, cr.GetIdentifier(), instanceType.ToString())

	// start with same labels as selector; note that this object gets modified by resources.AppendParentMeta()
	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	// prepare volume claims
	volumeClaims, err := getSplunkVolumeClaims(cr, spec, labels)
	if err != nil {
		return nil, err
	}
	for idx := range volumeClaims {
		volumeClaims[idx].ObjectMeta.Name = fmt.Sprintf("pvc-%s", volumeClaims[idx].ObjectMeta.Name)
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
				MatchLabels: selectLabels,
			},
			ServiceName:         GetSplunkServiceName(instanceType, cr.GetIdentifier(), true),
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
					SchedulerName: spec.SchedulerName,
					Containers: []corev1.Container{
						{
							Image:           spec.Image,
							ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
							Name:            "splunk",
							Ports:           ports,
							VolumeMounts:    getSplunkVolumeMounts(),
						},
					},
				},
			},
			VolumeClaimTemplates: volumeClaims,
		},
	}

	// append labels and annotations from parent
	resources.AppendParentMeta(statefulSet.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())

	// update statefulset's pod template with common splunk pod config
	updateSplunkPodTemplateWithConfig(&statefulSet.Spec.Template, cr, spec, instanceType, extraEnv)

	// make Splunk Enterprise object the owner
	statefulSet.SetOwnerReferences(append(statefulSet.GetOwnerReferences(), resources.AsOwner(cr)))

	return statefulSet, nil
}

// updateSplunkPodTemplateWithConfig modifies the podTemplateSpec object based on configuration of the Splunk Enterprise resource.
func updateSplunkPodTemplateWithConfig(podTemplateSpec *corev1.PodTemplateSpec, cr enterprisev1.MetaObject, spec *enterprisev1.CommonSplunkSpec, instanceType InstanceType, extraEnv []corev1.EnvVar) {

	// Add custom volumes to splunk containers
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

	// add defaults secrets to all splunk containers
	addSplunkVolumeToTemplate(podTemplateSpec, "secrets", corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName: GetSplunkSecretsName(cr.GetIdentifier(), instanceType),
		},
	})

	// add inline defaults to all splunk containers
	if spec.Defaults != "" {
		addSplunkVolumeToTemplate(podTemplateSpec, "defaults", corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: GetSplunkDefaultsName(cr.GetIdentifier(), instanceType),
				},
			},
		})
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
		splunkDefaults = fmt.Sprintf("%s,%s", splunkDefaults, spec.DefaultsURL)
	}
	if spec.Defaults != "" {
		splunkDefaults = fmt.Sprintf("%s,%s", splunkDefaults, "/mnt/splunk-defaults/default.yml")
	}

	// prepare container env variables
	env := []corev1.EnvVar{
		{Name: "SPLUNK_HOME", Value: "/opt/splunk"},
		{Name: "SPLUNK_START_ARGS", Value: "--accept-license"},
		{Name: "SPLUNK_DEFAULTS_URL", Value: splunkDefaults},
		{Name: "SPLUNK_HOME_OWNERSHIP_ENFORCEMENT", Value: "false"},
		{Name: "SPLUNK_ROLE", Value: instanceType.ToRole()},
	}

	// update variables for licensing, if configured
	if spec.LicenseURL != "" {
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_LICENSE_URI",
			Value: spec.LicenseURL,
		})
	}
	if instanceType != SplunkLicenseMaster && spec.LicenseMasterRef.Name != "" {
		licenseMasterURL := GetSplunkServiceName(SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
		if spec.LicenseMasterRef.Namespace != "" {
			licenseMasterURL = resources.GetServiceFQDN(spec.LicenseMasterRef.Namespace, licenseMasterURL)
		}
		env = append(env, corev1.EnvVar{
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: licenseMasterURL,
		})
	}

	// append URL for cluster master, if configured
	var clusterMasterURL string
	if instanceType == SplunkIndexer {
		clusterMasterURL = GetSplunkServiceName(SplunkClusterMaster, cr.GetIdentifier(), false)
	} else if instanceType != SplunkClusterMaster && spec.IndexerClusterRef.Name != "" {
		clusterMasterURL = GetSplunkServiceName(SplunkClusterMaster, spec.IndexerClusterRef.Name, false)
		if spec.IndexerClusterRef.Namespace != "" {
			clusterMasterURL = resources.GetServiceFQDN(spec.IndexerClusterRef.Namespace, clusterMasterURL)
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

// getSearchHeadExtraEnv returns extra environment variables used by search head clusters
func getSearchHeadExtraEnv(cr enterprisev1.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkSearchHead, cr.GetIdentifier(), replicas, false),
		}, {
			Name:  "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: GetSplunkStatefulsetURL(cr.GetNamespace(), SplunkSearchHead, cr.GetIdentifier(), 0, false),
		},
	}
}

// getIndexerExtraEnv returns extra environment variables used by search head clusters
func getIndexerExtraEnv(cr enterprisev1.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkIndexer, cr.GetIdentifier(), replicas, false),
		},
	}
}
