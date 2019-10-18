// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package deploy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ApplySparkDeployment creates or updates a Kubernetes Deployment for a given type of Spark instance.
func ApplySparkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, replicas int, envVariables []corev1.EnvVar, ports []corev1.ContainerPort) error {

	deployment, err := spark.GetSparkDeployment(cr, instanceType, replicas, envVariables, ports)
	if err != nil {
		return err
	}

	return ApplyDeployment(client, deployment)
}

// ApplyDeployment creates or updates a Kubernetes Deployment
func ApplyDeployment(client client.Client, deployment *appsv1.Deployment) error {

	var oldDeployment appsv1.Deployment
	namespacedName := types.NamespacedName{
		Namespace: deployment.Namespace,
		Name: deployment.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &oldDeployment)
	if err == nil {
		// found existing Deployment
		err = UpdateResource(client, deployment)
	} else {
		err = CreateResource(client, deployment)
	}

	return err
}
