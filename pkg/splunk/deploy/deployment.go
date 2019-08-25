// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package deploy

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
