/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/internal/controller/common"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	"github.com/splunk/splunk-operator/pkg/splunk/reconcile/kvservice"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KVServiceReconciler reconciles a KVService object
type KVServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

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
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=kvservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=kvservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=kvservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KVService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *KVServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "KVService")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "KVService")
	return kvservice.Apply(ctx, r.Client, req.NamespacedName, r.Recorder)
}
func (r *KVServiceReconciler) mapConfigMapToKVServices(ctx context.Context, obj client.Object) []reconcile.Request {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	var list enterpriseApi.KVServiceList
	if err := r.List(ctx, &list, client.InNamespace(cm.Namespace)); err != nil {
		log.FromContext(ctx).Error(err, "could not list KVServices for ConfigMap", "configMap", cm.Name)
		return nil
	}
	var requests []reconcile.Request
	for _, cr := range list.Items {
		for _, volume := range cr.Spec.Volumes {
			if common.VolumeReferencesConfigMap(volume, cm.Name) {
				requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}})
				break
			}
		}
	}
	return requests
}

func (r *KVServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorder("kvservice-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.KVService{}).
		WithEventFilter(predicate.Or(
			common.GenerationChangedPredicate(),
			common.AnnotationChangedPredicate(),
			common.LabelChangedPredicate(),
			common.SecretChangedPredicate(),
			common.DeploymentChangedPredicate(),
			common.PodChangedPredicate(),
			common.ConfigMapChangedPredicate(),
			common.CrdChangedPredicate(),
		)).
		Watches(&appsv1.Deployment{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.KVService{},
			)).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.KVService{},
			)).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.KVService{},
			)).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.KVService{},
			)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.mapConfigMapToKVServices)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterpriseApi.TotalWorker,
		}).
		Complete(r)
}
