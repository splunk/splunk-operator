package deploy

import (
	"fmt"

	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"git.splunk.com/splunk-operator/pkg/splunk/resources"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


func CreateSplunkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar) error {

	labels := enterprise.GetSplunkAppLabels(identifier, instanceType.ToString())
	deploymentName := enterprise.GetSplunkDeploymentName(instanceType, identifier)

	volumeClaims, err := enterprise.GetSplunkVolumeClaims(cr, instanceType, labels)
	if err != nil {
		return err
	}
	for idx, _ := range volumeClaims {
		volumeClaims[idx].ObjectMeta.Name = fmt.Sprintf("pvc-%s-%s", volumeClaims[idx].ObjectMeta.Name, deploymentName)
		resources.AddOwnerRefToObject(&volumeClaims[idx], resources.AsOwner(cr))
		err = resources.CreateResource(client, &volumeClaims[idx])
		if err != nil {
			return err
		}
	}

	deployment, err := enterprise.GetSplunkDeployment(cr, instanceType, deploymentName, labels, replicas, envVariables)
	if err != nil {
		return err
	}

	err = resources.CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}


func CreateSparkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, ports []corev1.ContainerPort) error {

	deployment, err := spark.GetSparkDeployment(cr, instanceType, identifier, replicas, envVariables, ports)
	if err != nil {
		return err
	}

	err = resources.CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}
