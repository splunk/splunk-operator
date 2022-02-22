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

package controllers

import (
	"context"
	"time"

	"github.com/pkg/errors"
	common "github.com/splunk/splunk-operator/controllers/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	enterprisev3 "github.com/splunk/splunk-operator/api/v3"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// SearchHeadClusterReconciler reconciles a SearchHeadCluster object
type SearchHeadClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=searchheadclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=searchheadclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=searchheadclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/exec,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SearchHeadCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *SearchHeadClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// your logic here
	reconcileCounters.With(getPrometheusLabels(req, "SearchHeadCluster")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "SearchHeadCluster")

	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("searchheadcluster", req.NamespacedName)
	reqLogger.Info("start")

	// Fetch the SearchHeadCluster
	instance := &enterprisev3.SearchHeadCluster{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request.  Owned objects are automatically
			// garbage collected. For additional cleanup logic use
			// finalizers.  Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, errors.Wrap(err, "could not load search head cluster data")
	}

	// If the reconciliation is paused, requeue
	annotations := instance.GetAnnotations()
	if annotations != nil {
		if _, ok := annotations[enterprisev3.SearchHeadClusterPausedAnnotation]; ok {
			return ctrl.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
		}
	}

	return enterprise.ApplySearchHeadCluster(ctx, r.Client, instance)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SearchHeadClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev3.SearchHeadCluster{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
			common.LabelChangedPredicate(),
			common.SecretChangedPredicate(),
			common.ConfigMapChangedPredicate(),
			common.StatefulsetChangedPredicate(),
			common.PodChangedPredicate(),
		)).
		Watches(&source.Kind{Type: &appsv1.StatefulSet{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterprisev3.SearchHeadCluster{},
			}).
		Watches(&source.Kind{Type: &corev1.Secret{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterprisev3.SearchHeadCluster{},
			}).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterprisev3.SearchHeadCluster{},
			}).
		Watches(&source.Kind{Type: &corev1.Pod{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterprisev3.SearchHeadCluster{},
			}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterprisev3.TotalWorker,
		}).
		Complete(r)
}
