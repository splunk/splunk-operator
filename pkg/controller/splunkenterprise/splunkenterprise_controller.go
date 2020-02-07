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

package splunkenterprise

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/deploy"
)

var log = logf.Log.WithName("controller_splunkenterprise")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new SplunkEnterprise Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	// Use a new go client to work-around issues with the operator sdk design.
	// If WATCH_NAMESPACE is empty for monitoring cluster-wide splunkenterprise resources,
	// the default caching client will attempt to list all resources in all namespaces for
	// any get requests, even if the request is namespace-scoped.
	options := client.Options{}
	client, err := client.New(mgr.GetConfig(), options)
	if err != nil {
		return err
	}
	reconciler := ReconcileSplunkEnterprise{
		client: client,
		scheme: mgr.GetScheme(),
	}
	return add(mgr, &reconciler)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("splunkenterprise-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource SplunkEnterprise
	err = c.Watch(&source.Kind{Type: &enterprisev1.SplunkEnterprise{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary custom resources and requeue the owner SplunkEnterprise
	err = c.Watch(&source.Kind{Type: &enterprisev1.Indexer{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &enterprisev1.SplunkEnterprise{},
	})
	if err != nil {
		return err
	}
	err = c.Watch(&source.Kind{Type: &enterprisev1.SearchHead{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &enterprisev1.SplunkEnterprise{},
	})
	if err != nil {
		return err
	}
	err = c.Watch(&source.Kind{Type: &enterprisev1.Spark{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &enterprisev1.SplunkEnterprise{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileSplunkEnterprise implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileSplunkEnterprise{}

// ReconcileSplunkEnterprise reconciles a SplunkEnterprise object
type ReconcileSplunkEnterprise struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a SplunkEnterprise object and makes changes based on the state read
// and what is in the SplunkEnterprise.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileSplunkEnterprise) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling SplunkEnterprise")

	// Fetch the SplunkEnterprise instance
	instance := &enterprisev1.SplunkEnterprise{}
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

	//instance.TypeMeta.APIVersion = "enterprise.splunk.com/v1alpha2"
	//instance.TypeMeta.Kind = "SplunkEnterprise"
	reqLogger.Info("Found instance", "APIVersion", instance.TypeMeta.APIVersion, "Kind", instance.TypeMeta.Kind)

	err = deploy.ApplySplunkEnterprise(instance, r.client)
	if err != nil {
		reqLogger.Error(err, "SplunkEnterprise reconciliation failed")
		return reconcile.Result{}, err
	}

	reqLogger.Info("SplunkEnterprise reconciliation complete")
	return reconcile.Result{}, nil
}
