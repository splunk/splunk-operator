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


func CreateSplunkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar) error {

	labels := enterprise.GetSplunkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	requirements, err := GetSplunkRequirements(cr)
	if err != nil {
		return err
	}

	volumeClaims, err := GetSplunkVolumeClaims(cr, instanceType, labels)
	if err != nil {
		return err
	}
	for idx, _ := range volumeClaims {
		volumeClaims[idx].ObjectMeta.Name = fmt.Sprintf("pvc-%s", volumeClaims[idx].ObjectMeta.Name)
	}

	statefulSet := &v1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: enterprise.GetSplunkStatefulsetName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: enterprise.GetSplunkHeadlessServiceName(instanceType, identifier),
			Replicas: &replicas32,
			PodManagementPolicy: "Parallel",
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
				},
			},
			VolumeClaimTemplates: volumeClaims,
		},
	}

	AddOwnerRefToObject(statefulSet, AsOwner(cr))

	err = UpdateSplunkPodTemplateWithConfig(&statefulSet.Spec.Template, cr, instanceType)
	if err != nil {
		return err
	}

	err = CreateResource(client, statefulSet)
	if err != nil {
		return err
	}

	err = CreateService(cr, client, instanceType, identifier, true)
	if err != nil {
		return err
	}

	return nil
}


func CreateSparkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, containerPorts []corev1.ContainerPort, servicePorts []corev1.ServicePort) error {

	labels := spark.GetSparkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	requirements, err := GetSparkRequirements(cr)
	if err != nil {
		return err
	}

	statefulSet := &v1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: spark.GetSparkStatefulsetName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: spark.GetSparkHeadlessServiceName(instanceType, identifier),
			Replicas: &replicas32,
			PodManagementPolicy: "Parallel",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: spark.GetSparkImage(cr),
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name: "spark",
							Ports: containerPorts,
							Env: envVariables,
							Resources: requirements,
						},
					},
				},
			},
		},
	}

	AddOwnerRefToObject(statefulSet, AsOwner(cr))

	err = UpdatePodTemplateWithConfig(&statefulSet.Spec.Template, cr)
	if err != nil {
		return err
	}

	err = CreateResource(client, statefulSet)
	if err != nil {
		return err
	}

	err = CreateSparkService(cr, client, instanceType, identifier, true, servicePorts)
	if err != nil {
		return err
	}

	return nil
}
