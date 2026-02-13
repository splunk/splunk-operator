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
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
)

const (
	retryDelay = time.Second * 15
)

// PostgresDatabaseReconciler reconciles a PostgresDatabase object
type PostgresDatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases/finalizers,verbs=update
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch

func (r *PostgresDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgresDatabase", "name", req.Name, "namespace", req.Namespace)

	// Fetch the PostgresDatabase CR details
	postgresDB := &enterprisev4.PostgresDatabase{}
	if err := r.Get(ctx, req.NamespacedName, postgresDB); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PostgresDatabase resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get a PostgresDatabase ", req.Name)
		return ctrl.Result{}, err
	}

	// Fetch the PostgresCluster CR details
	cluster := &enterprisev4.PostgresCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: req.Namespace}, cluster); err != nil {
		if errors.IsNotFound(err) {
			meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
				Type:    "ClusterReady",
				Status:  metav1.ConditionFalse,
				Reason:  "NotFound",
				Message: "Cluster CR not found",
			})
			postgresDB.Status.Phase = "Pending"
			if err := r.Status().Update(ctx, postgresDB); err != nil {
				logger.Error(err, "Failed to update Database status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		logger.Error(err, "Failed to fetch a Cluster ", postgresDB.Spec.ClusterRef.Name)
		meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
			Type:    "ClusterReady",
			Status:  metav1.ConditionFalse,
			Reason:  "ClusterInfoFetchNotPossible",
			Message: "Can't find the Cluster CR due to transient errors",
		})
		postgresDB.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to update Database status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	// Check clusterCR.Status.Phase
	if cluster.Status.Phase != "Ready" {
		logger.Info("Cluster not ready! Status:", cluster.Status.Phase)
		meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
			Type:    "ClusterReady",
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "Cluster is not in ready state yet",
		})
		postgresDB.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to update Database status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	// Cluster ready update status and start provisioning
	meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
		Type:    "ClusterReady",
		Status:  metav1.ConditionTrue,
		Reason:  "Available",
		Message: "Cluster is operational",
	})
	postgresDB.Status.Phase = "Provisioning"
	if err := r.Status().Update(ctx, postgresDB); err != nil {
		logger.Error(err, "Failed to update Database status")
		return ctrl.Result{}, err
	}
	for _, dbSpec := range postgresDB.Spec.Databases {
		logger.Info("Processing database", "database", dbSpec.Name)

		cnpgDBName := fmt.Sprintf("%s-%s", postgresDB.Name, dbSpec.Name)

		// Create CNPG Database CR
		cnpgDB := &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cnpgDBName,
				Namespace: postgresDB.Namespace,
			},
			Spec: cnpgv1.DatabaseSpec{
				Name:  dbSpec.Name,
				Owner: fmt.Sprintf("%s_admin", dbSpec.Name),
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Spec.CNPGClusterRef.Name,
				},
			},
		}

		// Set owner reference for cascade deletion
		if err := controllerutil.SetControllerReference(postgresDB, cnpgDB, r.Scheme); err != nil {
			logger.Error(err, "Failed to set owner reference")
			return ctrl.Result{}, err
		}

		// Create or update database
		if err := r.Create(ctx, cnpgDB); err != nil {
			if errors.IsAlreadyExists(err) {
				logger.Info("Database already exists, updating", "database", dbSpec.Name)

				// Fetch existing and update
				currentDB := &cnpgv1.Database{}
				if err := r.Get(ctx, types.NamespacedName{
					Name:      cnpgDBName,
					Namespace: postgresDB.Namespace,
				}, currentDB); err != nil {
					logger.Error(err, "Failed to get existing CNPG Database", "name", cnpgDBName)
					return ctrl.Result{}, err
				}

				// Update spec
				currentDB.Spec = cnpgDB.Spec
				if err := r.Update(ctx, currentDB); err != nil {
					logger.Error(err, "Failed to update CNPG Database", "name", cnpgDBName)
					return ctrl.Result{}, err
				}
			} else {
				// Real error (not AlreadyExists)
				logger.Error(err, "Failed to create CNPG Database", "name", cnpgDBName)
				return ctrl.Result{}, err
			}
		}

		logger.Info("CNPG Database created/updated successfully", "database", dbSpec.Name)
	}
	meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
		Type:    "DatabaseReady",
		Status:  metav1.ConditionFalse,
		Reason:  "Provisioning",
		Message: "Waiting for CNPG to provision databases",
	})
	postgresDB.Status.Phase = "Provisioning"
	if err := r.Status().Update(ctx, postgresDB); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}
	allReady := true
	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := fmt.Sprintf("%s-%s", postgresDB.Name, dbSpec.Name)

		// Fetch the CNPG Database to check its status
		cnpgDB := &cnpgv1.Database{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      cnpgDBName,
			Namespace: postgresDB.Namespace,
		}, cnpgDB); err != nil {
			logger.Error(err, "Failed to get CNPG Database status", "database", dbSpec.Name)
			allReady = false
			break
		}

		// Check if CNPG reports this database as ready
		if cnpgDB.Status.Applied == nil || !*cnpgDB.Status.Applied {
			logger.Info("Database not ready yet", "database", dbSpec.Name)
			allReady = false
			break
		}
	}
	if allReady {
		// All databases are actually provisioned by CNPG
		meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
			Type:    "DatabasesReady",
			Status:  metav1.ConditionTrue,
			Reason:  "AllProvisioned",
			Message: fmt.Sprintf("All %d database(s) are ready", len(postgresDB.Spec.Databases)),
		})
		postgresDB.Status.Phase = "Ready"
		if err := r.Status().Update(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("Databases not ready yet. Requeue...")
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresDatabase{}).
		Named("postgresdatabase").
		Complete(r)
}
