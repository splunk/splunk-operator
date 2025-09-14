/*
Copyright 2021.

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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/database/reconcile"
)

// DatabaseClusterReconciler reconciles a DatabaseCluster object
type DatabaseClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclaim,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
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
// the DatabaseCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *DatabaseClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)
	var db enterprisev4.DatabaseCluster
	if err := r.Get(ctx, req.NamespacedName, &db); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	ra, err := reconcile.ReconcileDatabaseCluster(ctx, r.Client, &db)
	if err2 := r.Status().Update(ctx, &db); err2 != nil {
		return ctrl.Result{}, err2
	}
	if ra == 0 {
		ra = 2 * time.Minute
	}
	return ctrl.Result{RequeueAfter: ra}, err

}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.DatabaseCluster{}).
		Named("databasecluster").
		Complete(r)
}
