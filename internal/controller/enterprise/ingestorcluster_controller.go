// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/internal/controller/common"
	"github.com/splunk/splunk-operator/pkg/config"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	ingestorcluster "github.com/splunk/splunk-operator/pkg/splunk/reconcile/ingestorcluster"
	certs "github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

// IngestorClusterReconciler reconciles a IngestorCluster object
type IngestorClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=ingestorclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=ingestorclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=ingestorclusters/finalizers,verbs=update

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=queues;objectstorages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=queues/status;objectstorages/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=queues/finalizers;objectstorages/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods/eviction,verbs=create
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=list;create;watch

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
	var err error

	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "IngestorCluster")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "IngestorCluster")
	defer func() {
		if err != nil {
			metrics.ReconcileErrorCounter.With(metrics.GetPrometheusLabels(req, "IngestorCluster")).Inc()
		}
	}()

	result, err := ingestorcluster.Apply(ctx, r.Client, req.NamespacedName, r.Recorder)
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *IngestorClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	bldr := ctrl.NewControllerManagedBy(mgr).
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
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				secret, ok := obj.(*corev1.Secret)
				if !ok {
					return nil
				}

				// Only consider ingestor clusters in the same namespace as the Secret
				cr := &enterpriseApi.IngestorCluster{}
				cr.SetNamespace(secret.Namespace)
				list, err := k8sops.GetIngestorClusterList(ctx, r.Client, cr, []client.ListOption{client.InNamespace(secret.Namespace)})
				if err != nil {
					return nil
				}

				var reqs []reconcile.Request
				for _, ic := range list.Items {
					if ic.Spec.QueueRef.Name == "" {
						continue
					}

					queueNS := ic.Spec.QueueRef.Namespace
					if queueNS == "" {
						queueNS = ic.Namespace
					}

					queue, err := k8sops.GetQueue(ctx, r.Client, &ic, types.NamespacedName{
						Name:      ic.Spec.QueueRef.Name,
						Namespace: queueNS,
					})
					if err != nil {
						continue
					}

					if queue.Spec.Provider != "sqs" && queue.Spec.Provider != "sqs_cp" {
						continue
					}

					if queue.Spec.SQS.SecretKeyRef != nil &&
						(queue.Spec.SQS.SecretKeyRef.AwsAccessKey.Name == secret.Name ||
							queue.Spec.SQS.SecretKeyRef.AwsSecretKey.Name == secret.Name) {
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
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				cm, ok := obj.(*corev1.ConfigMap)
				if !ok {
					return nil
				}
				listCR := &enterpriseApi.IngestorCluster{}
				listCR.SetNamespace(cm.Namespace)
				list, err := k8sops.GetIngestorClusterList(ctx, r.Client, listCR, []client.ListOption{client.InNamespace(cm.Namespace)})
				if err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, cr := range list.Items {
					for _, vol := range cr.Spec.Volumes {
						if common.VolumeReferencesConfigMap(vol, cm.Name) {
							reqs = append(reqs, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name:      cr.Name,
									Namespace: cr.Namespace,
								},
							})
							break
						}
					}
				}
				return reqs
			}),
		).
		Watches(&enterpriseApi.Queue{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				queue, ok := obj.(*enterpriseApi.Queue)
				if !ok {
					return nil
				}
				list, err := k8sops.GetIngestorClusterList(ctx, r.Client, &enterpriseApi.IngestorCluster{}, nil)
				if err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, ic := range list.Items {
					ns := ic.Spec.QueueRef.Namespace
					if ns == "" {
						ns = ic.Namespace
					}
					if ic.Spec.QueueRef.Name == queue.Name && ns == queue.Namespace {
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
		Watches(&enterpriseApi.ObjectStorage{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				os, ok := obj.(*enterpriseApi.ObjectStorage)
				if !ok {
					return nil
				}
				list, err := k8sops.GetIngestorClusterList(ctx, r.Client, &enterpriseApi.IngestorCluster{}, nil)
				if err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, ic := range list.Items {
					ns := ic.Spec.ObjectStorageRef.Namespace
					if ns == "" {
						ns = ic.Namespace
					}
					if ic.Spec.ObjectStorageRef.Name == os.Name && ns == os.Namespace {
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
		})

	if config.DefaultMutableFeatureGate.Enabled(config.CertManagement) {
		bldr = bldr.Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(
				certs.CertSecretMapper(mgr.GetClient(), &enterpriseApi.IngestorClusterList{})))
	}

	return bldr.
		Named("ingestor-cluster-controller").
		Complete(r)
}
