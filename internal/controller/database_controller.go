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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
)

const (
	retryDelay = time.Second * 15
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databases/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=clusters,verbs=get;list;watch
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Database", "name", req.Name, "namespace", req.Namespace)

	// Fetch the Database CR details
	db := &enterprisev4.Database{}
	if err := r.Get(ctx, req.NamespacedName, db); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Database resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get a Database ", db.Name)
		return ctrl.Result{}, err
	}

	// Fetch the Cluster CR details - change to Cluster once merged in another PR
	cluster := &enterprisev4.ClusterClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: db.Spec.ClusterRef.Name, Namespace: req.Namespace}, cluster); err != nil {
		if errors.IsNotFound(err) {
			meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
				Type:    "ClusterReady",
				Status:  metav1.ConditionFalse,
				Reason:  "NotFound",
				Message: "Cluster CR not found",
			})
			db.Status.Phase = "Pending"
			if err := r.Status().Update(ctx, db); err != nil {
				logger.Error(err, "Failed to update Database status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		logger.Error(err, "Failed to fetch a Cluster ", db.Spec.ClusterRef.Name)
		meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
			Type:    "ClusterReady",
			Status:  metav1.ConditionFalse,
			Reason:  "ClusterInfoFetchNotPossible",
			Message: "Can't find the Cluster CR due to transient errors",
		})
		db.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, db); err != nil {
			logger.Error(err, "Failed to update Database status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	// Check clusterCR.Status.Phase
	if cluster.Status.Phase != "Ready" {
		logger.Info("Cluster not ready! Status:", cluster.Status.Phase)
		meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
			Type:    "ClusterReady",
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "Cluster is not in ready state yet",
		})
		db.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, db); err != nil {
			logger.Error(err, "Failed to update Database status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	// Cluster ready update status and start provisioning
	meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
		Type:    "ClusterReady",
		Status:  metav1.ConditionTrue,
		Reason:  "Available",
		Message: "Cluster is operational",
	})
	db.Status.Phase = "Provisioning"
	if err := r.Status().Update(ctx, db); err != nil {
		logger.Error(err, "Failed to update Database status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.Database{}).
		Named("database").
		Complete(r)
}
