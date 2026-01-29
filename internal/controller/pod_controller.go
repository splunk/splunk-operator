/*
Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// PodReconciler reconciles Splunk pods with finalizers to ensure proper cleanup
// during pod deletion (decommission, peer removal, PVC cleanup)
//
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get
//+kubebuilder:rbac:groups="",resources=pods/finalizers,verbs=update
type PodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile handles pod lifecycle events for pods with the splunk.com/pod-cleanup finalizer
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("PodReconciler").WithValues("pod", req.NamespacedName)

	scopedLog.Info("PodReconciler.Reconcile called")

	// Fetch the pod
	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		// Pod not found, likely deleted - this is normal
		scopedLog.Info("Pod not found", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	scopedLog.Info("Pod fetched", "hasFinalizer", hasFinalizer(pod, enterprise.PodCleanupFinalizer), "deletionTimestamp", pod.DeletionTimestamp)

	// Only process pods with our finalizer
	if !hasFinalizer(pod, enterprise.PodCleanupFinalizer) {
		scopedLog.Info("Pod does not have finalizer, skipping")
		return ctrl.Result{}, nil
	}

	// Only process pods that are being deleted
	if pod.DeletionTimestamp == nil {
		scopedLog.Info("Pod not being deleted, skipping")
		return ctrl.Result{}, nil
	}

	scopedLog.Info("Processing pod deletion with finalizer cleanup")

	// Call the pod deletion handler
	err := enterprise.HandlePodDeletion(ctx, r.Client, pod)
	if err != nil {
		scopedLog.Error(err, "Failed to handle pod deletion, will retry")
		// Requeue with exponential backoff
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	scopedLog.Info("Successfully completed pod deletion cleanup")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Use a simpler predicate that only filters by finalizer presence
	// All other logic is handled in Reconcile() for better debugging
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				// Reconcile newly created pods with finalizer
				pod, ok := e.Object.(*corev1.Pod)
				return ok && hasFinalizer(pod, enterprise.PodCleanupFinalizer)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Reconcile all updates to pods with finalizer
				// Reconcile() will handle detailed filtering
				podNew, ok := e.ObjectNew.(*corev1.Pod)
				if !ok {
					return false
				}
				// Reconcile if pod has finalizer OR had finalizer (for cleanup)
				podOld, _ := e.ObjectOld.(*corev1.Pod)
				hasFinalizerNew := hasFinalizer(podNew, enterprise.PodCleanupFinalizer)
				hasFinalizerOld := podOld != nil && hasFinalizer(podOld, enterprise.PodCleanupFinalizer)
				return hasFinalizerNew || hasFinalizerOld
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				// Don't reconcile on delete events (pod is already gone)
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				// Don't watch generic events
				return false
			},
		}).
		Complete(r)
}

// hasFinalizer checks if the pod has the specified finalizer
func hasFinalizer(pod *corev1.Pod, finalizer string) bool {
	for _, f := range pod.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}
