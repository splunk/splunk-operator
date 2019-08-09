// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package deploy

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


// CreateSplunkStatefulSet creates a Kubernetes StatefulSet for a given type of Splunk Enterprise instance (indexers or search heads).
func CreateSplunkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, replicas int, envVariables []corev1.EnvVar) error {

	statefulSet, err := enterprise.GetSplunkStatefulSet(cr, instanceType, replicas, envVariables)
	if err != nil {
		return err
	}

	err = CreateResource(client, statefulSet)
	if err != nil {
		return err
	}

	return nil
}


// CreateSparkStatefulSet creates a Kubernetes StatefulSet for a given type of Spark instance (master or workers).
func CreateSparkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, replicas int, envVariables []corev1.EnvVar, containerPorts []corev1.ContainerPort) error {

	statefulSet, err := spark.GetSparkStatefulSet(cr, instanceType, replicas, envVariables, containerPorts)
	if err != nil {
		return err
	}

	err = CreateResource(client, statefulSet)
	if err != nil {
		return err
	}

	return nil
}
