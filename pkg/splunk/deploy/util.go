// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package deploy

import (
	"log"
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	log.Printf("Created %s/%s in namespace %s\n", obj.GetObjectKind(), obj.GetObjectMeta().GetName(), obj.GetObjectMeta().GetNamespace())

	return nil
}
