package resources

import (
	"fmt"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


func CreateSplunkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, DNSConfigSearches []string) error {

	labels := enterprise.GetSplunkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)
	deploymentName := enterprise.GetSplunkDeploymentName(instanceType, identifier)

	requirements, err := GetSplunkRequirements(cr)
	if err != nil {
		return err
	}

	volumeClaims, err := GetSplunkVolumeClaims(cr, instanceType, labels)
	if err != nil {
		return err
	}
	for idx, _ := range volumeClaims {
		volumeClaims[idx].ObjectMeta.Name = fmt.Sprintf("pvc-%s-%s", volumeClaims[idx].ObjectMeta.Name, deploymentName)
		AddOwnerRefToObject(&volumeClaims[idx], AsOwner(cr))
		err = CreateResource(client, &volumeClaims[idx])
		if err != nil {
			return err
		}
	}

	deployment := &v1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentName,
			Namespace: cr.Namespace,
		},
		Spec: v1.DeploymentSpec{
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
							Image: enterprise.GetSplunkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.Config.ImagePullPolicy),
							Name: "splunk",
							Ports: enterprise.GetSplunkContainerPorts(),
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

	if cr.Spec.Config.DefaultsConfigMapName != "" {
		AddConfigMapVolumeToPodTemplate(&deployment.Spec.Template, "splunk-defaults", cr.Spec.Config.DefaultsConfigMapName, enterprise.SPLUNK_DEFAULTS_MOUNT_LOCATION)
	}

	if cr.Spec.Config.SplunkLicense.VolumeSource != nil {
		AddLicenseVolumeToPodTemplate(&deployment.Spec.Template, "splunk-license", cr.Spec.Config.SplunkLicense.VolumeSource, enterprise.LICENSE_MOUNT_LOCATION)
	}

	if DNSConfigSearches != nil {
		deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
		deployment.Spec.Template.Spec.DNSConfig = &corev1.PodDNSConfig{
			Searches: DNSConfigSearches,
		}
	}

	if cr.Spec.Config.EnableDFS && (instanceType == enterprise.SPLUNK_SEARCH_HEAD || instanceType == enterprise.SPLUNK_STANDALONE) {
		err = AddDFCToPodTemplate(&deployment.Spec.Template, cr)
		if err != nil {
			return err
		}
	}

	AddOwnerRefToObject(deployment, AsOwner(cr))

	err = CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}


func CreateSparkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, ports []corev1.ContainerPort) error {

	labels := spark.GetSparkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	requirements, err := GetSparkRequirements(cr)
	if err != nil {
		return err
	}

	deployment := &v1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: spark.GetSparkDeploymentName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Replicas: &replicas32,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Hostname: spark.GetSparkServiceName(instanceType, identifier),
					Containers: []corev1.Container{
						{
							Image: spark.GetSparkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.Config.ImagePullPolicy),
							Name: "spark",
							Ports: ports,
							Env: envVariables,
							Resources: requirements,
						},
					},
				},
			},
		},
	}

	AddOwnerRefToObject(deployment, AsOwner(cr))

	err = CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}
