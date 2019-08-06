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


func CreateSplunkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar) error {

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
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
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

	AddOwnerRefToObject(deployment, AsOwner(cr))

	err = UpdateSplunkPodTemplateWithConfig(&deployment.Spec.Template, cr, instanceType)
	if err != nil {
		return err
	}

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
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
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

	err = UpdatePodTemplateWithConfig(&deployment.Spec.Template, cr)
	if err != nil {
		return err
	}

	err = CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}
