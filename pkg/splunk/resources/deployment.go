package resources

import (
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

	deployment := &v1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: enterprise.GetSplunkDeploymentName(instanceType, identifier),
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
							Name: "splunk",
							Ports: enterprise.GetSplunkContainerPorts(),
							Env: envVariables,
						},
					},
					ImagePullSecrets: enterprise.GetImagePullSecrets(),
				},
			},
		},
	}

	if cr.Spec.Config.DefaultsConfigMapName != "" {
		AddConfigMapVolumeToPodTemplate(&deployment.Spec.Template, "splunk-defaults", cr.Spec.Config.DefaultsConfigMapName, "/tmp/defaults")
	}

	if DNSConfigSearches != nil {
		deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
		deployment.Spec.Template.Spec.DNSConfig = &corev1.PodDNSConfig{
			Searches: DNSConfigSearches,
		}
	}

	AddOwnerRefToObject(deployment, AsOwner(cr))

	err := CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}


func CreateSparkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, ports []corev1.ContainerPort) error {

	labels := spark.GetSparkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

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
							Image: spark.SPLUNK_SPARK_IMAGE,
							Name: "spark",
							Ports: ports,
							Env: envVariables,
						},
					},
					ImagePullSecrets: enterprise.GetImagePullSecrets(),
				},
			},
		},
	}

	AddOwnerRefToObject(deployment, AsOwner(cr))

	err := CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}