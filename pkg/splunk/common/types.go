// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Phase is used to represent the current phase of a custom resource
// +kubebuilder:validation:Enum=Pending;Ready;Updating;ScalingUp;ScalingDown;Terminating;Error
type Phase string

const (
	// PhasePending means a custom resource has just been created and is not yet ready
	PhasePending Phase = "Pending"

	// PhaseReady means a custom resource is ready and up to date
	PhaseReady Phase = "Ready"

	// PhaseUpdating means a custom resource is in the process of updating to a new desired state (spec)
	PhaseUpdating Phase = "Updating"

	// PhaseScalingUp means a customer resource is in the process of scaling up
	PhaseScalingUp Phase = "ScalingUp"

	// PhaseScalingDown means a customer resource is in the process of scaling down
	PhaseScalingDown Phase = "ScalingDown"

	// PhaseTerminating means a customer resource is in the process of being removed
	PhaseTerminating Phase = "Terminating"

	// PhaseError means an error occured with custom resource management
	PhaseError Phase = "Error"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// Spec defines the desired state of parameters that are common across all CRD types
type Spec struct {
	// Image to use for Splunk pod containers (overrides RELATED_IMAGE_SPLUNK_ENTERPRISE environment variables)
	Image string `json:"image"`

	// Sets pull policy for all images (either “Always” or the default: “IfNotPresent”)
	// +kubebuilder:validation:Enum=Always;IfNotPresent
	ImagePullPolicy string `json:"imagePullPolicy"`

	// Name of Scheduler to use for pod placement (defaults to “default-scheduler”)
	SchedulerName string `json:"schedulerName"`

	// Kubernetes Affinity rules that control how pods are assigned to particular nodes.
	Affinity corev1.Affinity `json:"affinity"`

	// Pod's tolerations for Kubernetes node's taint
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// resource requirements for the pod containers
	Resources corev1.ResourceRequirements `json:"resources"`

	// ServiceTemplate is a template used to create Kubernetes services
	ServiceTemplate corev1.Service `json:"serviceTemplate"`
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *Spec) DeepCopyInto(out *Spec) {
	out.Image = in.Image
	out.ImagePullPolicy = in.ImagePullPolicy
	out.SchedulerName = in.SchedulerName

	out.Affinity.DeepCopyInto(&in.Affinity)
	out.Resources.DeepCopyInto(&in.Resources)
	out.ServiceTemplate.DeepCopyInto(&in.ServiceTemplate)

	if in.Tolerations != nil {
		in, out := &in.Tolerations, &out.Tolerations
		*out = make([]corev1.Toleration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}

	return
}

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
	Update(context.Context, ControllerClient, *appsv1.StatefulSet, int32) (Phase, error)

	// PrepareScaleDown prepares pod to be removed via scale down event; it returns true when ready
	PrepareScaleDown(context.Context, int32) (bool, error)

	// PrepareRecycle prepares pod to be recycled for updates; it returns true when ready
	PrepareRecycle(context.Context, int32) (bool, error)

	// FinishRecycle completes recycle event for pod and returns true, or returns false if nothing to do
	FinishRecycle(context.Context, int32) (bool, error)
}
