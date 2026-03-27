/*
Copyright 2026.

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
	"reflect"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	dbadapter "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter"
	dbcore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// PostgresDatabaseReconciler reconciles a PostgresDatabase object.
type PostgresDatabaseReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const (
	DatabaseTotalWorker int = 2
)

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases/finalizers,verbs=update
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *PostgresDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	postgresDB := &enterprisev4.PostgresDatabase{}
	if err := r.Get(ctx, req.NamespacedName, postgresDB); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PostgresDatabase resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	rc := &dbcore.ReconcileContext{Client: r.Client, Scheme: r.Scheme, Recorder: r.Recorder}
	return dbcore.PostgresDatabaseService(ctx, rc, postgresDB, dbadapter.NewDBRepository)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&cnpgv1.Database{},
		".metadata.controller",
		func(obj client.Object) []string {
			owner := metav1.GetControllerOf(obj)
			if owner == nil {
				return nil
			}
			if owner.APIVersion != enterprisev4.GroupVersion.String() || owner.Kind != "PostgresDatabase" {
				return nil
			}
			return []string{owner.Name}
		},
	); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresDatabase{}, builder.WithPredicates(
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.Funcs{
					UpdateFunc: func(e event.UpdateEvent) bool {
						return !reflect.DeepEqual(
							e.ObjectOld.GetFinalizers(),
							e.ObjectNew.GetFinalizers(),
						)
					},
				},
			),
		)).
		Owns(&cnpgv1.Database{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Named("postgresdatabase").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: DatabaseTotalWorker,
		}).
		Complete(r)
}
