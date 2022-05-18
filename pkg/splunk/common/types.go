// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MetaObject is used to represent a common interfaces for Kubernetes resources
type MetaObject interface {
	metav1.Object
	schema.ObjectKind
	runtime.Object
	GetObjectMeta() metav1.Object
}

// The ControllerClient interfaces implements methods of the Kubernetes controller-runtime client
type ControllerClient interface {
	client.Client
}

// StatefulSetPodManager is used to manage the pods within a StatefulSet
type StatefulSetPodManager interface {
	// Update handles all updates for a statefulset and all of its pods
	Update(context.Context, ControllerClient, *appsv1.StatefulSet, int32) (enterpriseApi.Phase, error)

	// PrepareScaleDown prepares pod to be removed via scale down event; it returns true when ready
	PrepareScaleDown(context.Context, int32) (bool, error)

	// PrepareRecycle prepares pod to be recycled for updates; it returns true when ready
	PrepareRecycle(context.Context, int32) (bool, error)

	// FinishRecycle completes recycle event for pod and returns true, or returns false if nothing to do
	FinishRecycle(context.Context, int32) (bool, error)
}
