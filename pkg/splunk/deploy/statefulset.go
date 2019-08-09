package deploy

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"git.splunk.com/splunk-operator/pkg/splunk/resources"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


func CreateSplunkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar) error {

	statefulSet, err := enterprise.GetSplunkStatefulSet(cr, instanceType, identifier, replicas, envVariables)
	if err != nil {
		return err
	}

	err = resources.CreateResource(client, statefulSet)
	if err != nil {
		return err
	}

	return nil
}


func CreateSparkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, containerPorts []corev1.ContainerPort) error {

	statefulSet, err := spark.GetSparkStatefulSet(cr, instanceType, identifier, replicas, envVariables, containerPorts)
	if err != nil {
		return err
	}

	err = resources.CreateResource(client, statefulSet)
	if err != nil {
		return err
	}

	return nil
}
