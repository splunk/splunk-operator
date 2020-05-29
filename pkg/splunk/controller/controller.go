// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// SplunkController is used to represent common interfaces of Splunk controllers
type SplunkController interface {

	// GetInstance returns an instance of the custom resource managed by the controller
	GetInstance() splcommon.MetaObject

	// GetWatchTypes returns a list of types owned by the controller that it would like to receive watch events for
	GetWatchTypes() []runtime.Object

	// Reconcile is used to perform an idempotent reconciliation of the custom resource managed by this controller
	Reconcile(client.Client, splcommon.MetaObject) (reconcile.Result, error)
}

// AddToManager adds a specific Splunk Controller to the Manager.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func AddToManager(mgr manager.Manager, splctrl SplunkController) error {
	// Use a new go client to work-around issues with the operator sdk design.
	// If WATCH_NAMESPACE is empty for monitoring cluster-wide custom Splunk resources,
	// the default caching client will attempt to list all resources in all namespaces for
	// any get requests, even if the request is namespace-scoped.
	options := client.Options{}
	client, err := client.New(mgr.GetConfig(), options)
	if err != nil {
		return err
	}

	reconciler := splunkReconciler{
		client:  client,
		scheme:  mgr.GetScheme(),
		splctrl: splctrl,
	}

	// Create a new controller
	instance := reconciler.splctrl.GetInstance()
	c, err := controller.New(instance.GetObjectKind().GroupVersionKind().Kind, mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return err
	}

	// Watch for changes to primary custom resource
	err = c.Watch(&source.Kind{Type: instance}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources
	for _, t := range splctrl.GetWatchTypes() {
		err = c.Watch(&source.Kind{Type: t}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    instance,
		})
		if err != nil {
			return err
		}
	}

	return err
}

// blank assignment to verify that SplunkReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &splunkReconciler{}

// SplunkReconciler reconciles Splunk custom resources
type splunkReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	scheme  *runtime.Scheme
	splctrl SplunkController
}

// Reconcile reads that state of the cluster for a custom resource
// and makes changes based on the state read and what is in the Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r splunkReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	instance := r.splctrl.GetInstance()
	gvk := instance.GroupVersionKind()
	scopedLog := log.WithName("Reconcile").WithValues("Group", gvk.Group, "Version", gvk.Version, "Kind", gvk.Kind, "Namespace", request.Namespace, "Name", request.Name)
	scopedLog.Info("Reconciling custom resource")

	// Fetch the custom resource instance
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// ensure that APIVersion is defined (this gets wiped by client.Get)
	instance.SetGroupVersionKind(gvk)

	// call Reconcile method defined for the controller
	result, err := r.splctrl.Reconcile(r.client, instance)

	// log what happens next
	if err != nil {
		scopedLog.Error(err, "Reconciliation requeued", "RequeueAfter", result.RequeueAfter)
		return result, nil
	}
	if result.Requeue {
		scopedLog.Info("Reconciliation requeued", "RequeueAfter", result.RequeueAfter)
		return result, nil
	}

	scopedLog.Info("Reconciliation complete")
	return reconcile.Result{}, nil
}
