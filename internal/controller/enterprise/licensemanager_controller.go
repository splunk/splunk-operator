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
	licensemanager "github.com/splunk/splunk-operator/pkg/splunk/reconcile/licensemanager"
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

	"github.com/splunk/splunk-operator/pkg/config"
	certs "github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

// LicenseManagerReconciler reconciles a LicenseManager object
type LicenseManagerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=licensemanagers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=licensemanagers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=licensemanagers/finalizers,verbs=update
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
// the LicenseManager object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
// Reconcile delegates request-level reconciliation to the LicenseManager workflow.
func (r *LicenseManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "LicenseManager")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "LicenseManager")
	return licensemanager.Apply(ctx, r.Client, req.NamespacedName, r.Recorder)
}

// SetupWithManager sets up the controller with the Manager.
func (r *LicenseManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	bldr := ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.LicenseManager{}).
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
				&enterpriseApi.LicenseManager{},
			)).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.LicenseManager{},
			)).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.LicenseManager{},
			)).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				cm, ok := obj.(*corev1.ConfigMap)
				if !ok {
					return nil
				}
				var list enterpriseApi.LicenseManagerList
				if err := r.Client.List(ctx, &list, client.InNamespace(cm.Namespace)); err != nil {
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
		Watches(&corev1.Pod{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.LicenseManager{},
			)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterpriseApi.TotalWorker,
		})

	if config.DefaultMutableFeatureGate.Enabled(config.CertManagement) {
		bldr = bldr.Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(
				certs.CertSecretMapper(mgr.GetClient(), &enterpriseApi.LicenseManagerList{})))
	}

	return bldr.
		Named("license-manager-controller").
		Complete(r)
}
