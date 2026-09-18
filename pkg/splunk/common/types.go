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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
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
	GetObjectKind() schema.ObjectKind
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

	// FinishUpgrade finishes rolling upgrade process; it returns an error if upgrade process can't be finished
	FinishUpgrade(context.Context, int32) error
}

// StatefulSetScaleOutPlanner optionally controls the next replica target for a
// StatefulSet scale-out. Managers that do not implement this interface scale
// directly to the requested replica count.
type StatefulSetScaleOutPlanner interface {
	NextReplicas(context.Context, int32, int32) (ScaleOutPlan, error)
}

// StatefulSetScaleDownFinisher optionally gates further StatefulSet lifecycle
// work after Kubernetes has removed the highest ordinal.
type StatefulSetScaleDownFinisher interface {
	FinishScaleDown(context.Context, int32) (bool, error)
}

// StatefulSetRecycleOrderer optionally lets a manager defer recycling a
// specific ordinal during a rolling update, so the generic loop tries a
// lower ordinal instead this reconcile rather than stopping. Managers that
// do not implement this keep the default behavior: strictly
// highest-ordinal-first, one at a time.
type StatefulSetRecycleOrderer interface {
	// DeferRecycle reports whether ordinal n — whose pod revision does not
	// match updateRevision — should be skipped this reconcile in favor of a
	// lower ordinal. Returning true does not mean n will never be
	// recycled; it is re-evaluated fresh every reconcile.
	DeferRecycle(ctx context.Context, n int32, updateRevision string) (bool, error)
}

// ScaleOutPlan describes the next safe replica target and whether the requested
// scale-out has converged.
type ScaleOutPlan struct {
	Complete       bool
	TargetReplicas int32
}
