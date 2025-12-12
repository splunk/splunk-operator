/*
Copyright 2025.

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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/internal/controller/common"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// IngestorClusterReconciler reconciles a IngestorCluster object
type IngestorClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=ingestorclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=ingestorclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=ingestorclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the IngestorCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *IngestorClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "IngestorCluster")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "IngestorCluster")

	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("ingestorcluster", req.NamespacedName)

	// Fetch the IngestorCluster
	instance := &enterpriseApi.IngestorCluster{}
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
		return ctrl.Result{}, errors.Wrap(err, "could not load ingestor cluster data")
	}

	// If the reconciliation is paused, requeue
	annotations := instance.GetAnnotations()
	if annotations != nil {
		if _, ok := annotations[enterpriseApi.IngestorClusterPausedAnnotation]; ok {
			return ctrl.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
		}
	}

	reqLogger.Info("start", "CR version", instance.GetResourceVersion())

	result, err := ApplyIngestorCluster(ctx, r.Client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		reqLogger.Info("Requeued", "period(seconds)", int(result.RequeueAfter/time.Second))
	}

	return result, err
}

var ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
	return enterprise.ApplyIngestorCluster(ctx, client, instance)
}

// SetupWithManager sets up the controller with the Manager.
func (r *IngestorClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.IngestorCluster{}).
		WithEventFilter(predicate.Or(
			common.GenerationChangedPredicate(),
			common.AnnotationChangedPredicate(),
			common.LabelChangedPredicate(),
			common.SecretChangedPredicate(),
			common.ConfigMapChangedPredicate(),
			common.StatefulsetChangedPredicate(),
			common.PodChangedPredicate(),
			common.CrdChangedPredicate(),
		)).
		Watches(&appsv1.StatefulSet{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.IngestorCluster{},
			)).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.IngestorCluster{},
			)).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.IngestorCluster{},
			)).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.IngestorCluster{},
			)).
		Watches(&enterpriseApi.Bus{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				b, ok := obj.(*enterpriseApi.Bus)
				if !ok {
					return nil
				}
				var list enterpriseApi.IngestorClusterList
				if err := r.Client.List(ctx, &list); err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, ic := range list.Items {
					ns := ic.Spec.BusRef.Namespace
					if ns == "" {
						ns = ic.Namespace
					}
					if ic.Spec.BusRef.Name == b.Name && ns == b.Namespace {
						reqs = append(reqs, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      ic.Name,
								Namespace: ic.Namespace,
							},
						})
					}
				}
				return reqs
			}),
		).
		Watches(&enterpriseApi.LargeMessageStore{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				lms, ok := obj.(*enterpriseApi.LargeMessageStore)
				if !ok {
					return nil
				}
				var list enterpriseApi.IndexerClusterList
				if err := r.Client.List(ctx, &list); err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, ic := range list.Items {
					ns := ic.Spec.LargeMessageStoreRef.Namespace
					if ns == "" {
						ns = ic.Namespace
					}
					if ic.Spec.LargeMessageStoreRef.Name == lms.Name && ns == lms.Namespace {
						reqs = append(reqs, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      ic.Name,
								Namespace: ic.Namespace,
							},
						})
					}
				}
				return reqs
			}),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterpriseApi.TotalWorker,
		}).
		Complete(r)
}
