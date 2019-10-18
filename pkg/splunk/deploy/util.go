// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"log"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/api/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The ResourceObject type implements methods of runtime.Object and GetObjectMeta()
type ResourceObject interface {
	runtime.Object
	GetObjectMeta() metav1.Object
}

// CreateResource creates a new Kubernetes resource using the REST API.
func CreateResource(client client.Client, obj ResourceObject) error {
	err := client.Create(context.TODO(), obj)

	if err != nil && !errors.IsAlreadyExists(err) {
		log.Printf("Failed to create object : %v", err)
		return err
	}

	log.Printf("Created %s in namespace %s\n", obj.GetObjectMeta().GetName(), obj.GetObjectMeta().GetNamespace())

	return nil
}

// UpdateResource updates an existing Kubernetes resource using the REST API.
func UpdateResource(client client.Client, obj ResourceObject) error {
	err := client.Update(context.TODO(), obj)

	if err != nil && !errors.IsAlreadyExists(err) {
		log.Printf("Failed to update object : %v", err)
		return err
	}

	log.Printf("Updated %s in namespace %s\n", obj.GetObjectMeta().GetName(), obj.GetObjectMeta().GetNamespace())

	return nil
}

// ApplyConfigMap creates or updates a Kubernetes ConfigMap
func ApplyConfigMap(client client.Client, configMap *corev1.ConfigMap) error {

	var oldConfigMap corev1.ConfigMap
	namespacedName := types.NamespacedName{
		Namespace: configMap.Namespace,
		Name: configMap.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &oldConfigMap)
	if err == nil {
		// found existing ConfigMap: do nothing
		log.Printf("Found existing ConfigMap %s in namespace %s\n", configMap.GetObjectMeta().GetName(), configMap.GetObjectMeta().GetNamespace())
	} else {
		err = CreateResource(client, configMap)
	}

	return err
}

// ApplySecret creates or updates a Kubernetes Secret
func ApplySecret(client client.Client, secret *corev1.Secret) error {

	var oldSecret corev1.Secret
	namespacedName := types.NamespacedName{
		Namespace: secret.Namespace,
		Name: secret.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &oldSecret)
	if err == nil {
		// found existing Secret: do nothing
		log.Printf("Found existing Secret %s in namespace %s\n", secret.GetObjectMeta().GetName(), secret.GetObjectMeta().GetNamespace())
	} else {
		err = CreateResource(client, secret)
	}

	return err
}

// ApplyService creates or updates a Kubernetes Service
func ApplyService(client client.Client, service *corev1.Service) error {

	var oldService corev1.Service
	namespacedName := types.NamespacedName{
		Namespace: service.Namespace,
		Name: service.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &oldService)
	if err == nil {
		// found existing Service: do nothing
		log.Printf("Found existing Service %s in namespace %s\n", service.GetObjectMeta().GetName(), service.GetObjectMeta().GetNamespace())
	} else {
		err = CreateResource(client, service)
	}

	return err
}
