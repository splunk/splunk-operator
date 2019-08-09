// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

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


// CreateSplunkDeployment creates a Kubernetes Deployment for a given type of Splunk Enterprise instance.
func CreateSplunkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar) error {

	labels := enterprise.GetSplunkAppLabels(identifier, instanceType.ToString())
	deploymentName := enterprise.GetSplunkDeploymentName(instanceType, identifier)

	// get generic list of splunk persistent volume claims
	volumeClaims, err := enterprise.GetSplunkVolumeClaims(cr, instanceType, labels)
	if err != nil {
		return err
	}

	// inject deployment name into the volume claim names to make them unique
	for idx, _ := range volumeClaims {
		volumeClaim := volumeClaims[idx]
		volumeClaim.ObjectMeta.Name = fmt.Sprintf("pvc-%s-%s", volumeClaim.ObjectMeta.Name, deploymentName)
		volumeClaim.SetOwnerReferences(append(volumeClaim.GetOwnerReferences(), resources.AsOwner(cr)))
		err = CreateResource(client, &volumeClaim)
		if err != nil {
			return err
		}
	}

	// get configuration for kubernetes deployment resource
	deployment, err := enterprise.GetSplunkDeployment(cr, instanceType, deploymentName, labels, replicas, envVariables)
	if err != nil {
		return err
	}

	// create kubernetes deployment resource
	err = CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}


// CreateSparkDeployment creates a Kubernetes Deployment for a given type of Spark instance.
func CreateSparkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, replicas int, envVariables []corev1.EnvVar, ports []corev1.ContainerPort) error {

	deployment, err := spark.GetSparkDeployment(cr, instanceType, replicas, envVariables, ports)
	if err != nil {
		return err
	}

	err = CreateResource(client, deployment)
	if err != nil {
		return err
	}

	return nil
}
