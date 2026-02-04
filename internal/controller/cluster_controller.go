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
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	logs "sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=clusterclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop for Cluster resources.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logs.FromContext(ctx)
	logger.Info("Reconciling Cluster", "name", req.Name, "namespace", req.Namespace)

	// Fetch the Cluster instance
	cluster := &enterprisev4.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Cluster resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Cluster.")
		return ctrl.Result{}, err
	}

	// Fetch the corresponding ClusterClass
	clusterClass := &enterprisev4.ClusterClass{}
	if err := r.Get(ctx, client.ObjectKey{Name: cluster.Spec.Class}, clusterClass); err != nil {
		logger.Error(err, "Failed to get ClusterClass for Cluster.", "ClusterClass", cluster.Spec.Class)
		return ctrl.Result{}, err
	}

	// Merge ClusterClass spec into Cluster spec
	mergedConfig := r.getMergedConfig(clusterClass, cluster)

	if err := validateClusterConfig(mergedConfig, clusterClass); err != nil {
		logger.Error(err, "Merged config invalid")
		return ctrl.Result{}, err
	}

	// Check if the CNPG Cluster already exists and create one if not
	cnpgCluster := &cnpgv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      cluster.Name,
		Namespace: cluster.Namespace,
	}, cnpgCluster)

	if err != nil && errors.IsNotFound(err) {
		// Define a new CNPG Cluster
		logger.Info("Defining a new CNPG Cluster", "Cluster.Name", cluster.Name, "Cluster.Namespace", cluster.Namespace)
		cnpgCluster, err = r.defineNewCNPGCluster(clusterClass, cluster, mergedConfig)
		if err != nil {
			logger.Error(err, "Failed to define CNPG Cluster.")
			return ctrl.Result{}, err
		}

		// Create the CNPG Cluster
		logger.Info("Creating a new CNPG Cluster", "CNPGCluster.Name", cnpgCluster.Name)
		if err := r.Create(ctx, cnpgCluster); err != nil {
			logger.Error(err, "Failed to create CNPG Cluster.")
			return ctrl.Result{}, err
		}
		// Update Cluster status with provisioner reference
		cluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: "postgresql.cnpg.io/v1",
			Kind:       "Cluster",
			Namespace:  cnpgCluster.Namespace,
			Name:       cnpgCluster.Name,
			UID:        cnpgCluster.UID,
		}
		cluster.Status.Phase = "Provisioning"

		if err := r.Status().Update(ctx, cluster); err != nil {
			logger.Error(err, "Failed to update Cluster status after CNPG Cluster creation.")
			return ctrl.Result{}, err
		}

		logger.Info("CNPG Cluster created successfully", "CNPGCluster.Name", cnpgCluster.Name)
		// Requeue to check the status of the CNPG cluster
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get CNPG Cluster.")
		return ctrl.Result{}, err
	}

	// CNPG Cluster exists, ensure it matches the desired state
	logger.Info("CNPG Cluster already exists. Ensuring it is up to date.", "CNPGCluster.Name", cnpgCluster.Name)
	updated, err := r.ensureClusterUpToDate(ctx, clusterClass, cnpgCluster, mergedConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure CNPG Cluster is up to date.")
		return ctrl.Result{}, err
	}
	if updated {
		logger.Info("CNPG Cluster updated successfully", "CNPGCluster.Name", cnpgCluster.Name)
	} else {
		logger.Info("CNPG Cluster is already up to date", "CNPGCluster.Name", cnpgCluster.Name)
	}

	// Update CNPG cluster status if it was changed
	logger.Info("Updating Cluster status based on CNPG state", "Phase", cnpgCluster.Status.Phase)
	if err := r.updateClusterStatus(ctx, cluster, cnpgCluster); err != nil {
		logger.Error(err, "Failed to update Cluster status.")
		return ctrl.Result{}, err
	} else {
		logger.Info("Cluster is up to date", "Cluster.Name", cluster.Name)
	}

	return ctrl.Result{}, nil
}

// getMergedConfig merges the configuration from the ClusterClass into the ClusterSpec, giving precedence to the ClusterSpec values.
func (r *ClusterReconciler) getMergedConfig(clusterClass *enterprisev4.ClusterClass, cluster *enterprisev4.Cluster) *enterprisev4.ClusterSpec {
	instances := clusterClass.Spec.ClusterConfig.Instances
	engineConfig := clusterClass.Spec.ClusterConfig.PostgreSQLConfig
	postgresVersion := clusterClass.Spec.ClusterConfig.PostgresVersion
	resources := clusterClass.Spec.ClusterConfig.Resources
	storage := clusterClass.Spec.ClusterConfig.Storage
	pgHBA := clusterClass.Spec.ClusterConfig.PgHBA

	if cluster.Spec.Instances != nil {
		instances = cluster.Spec.Instances
	}
	if cluster.Spec.PostgresVersion != nil {
		postgresVersion = cluster.Spec.PostgresVersion
	}
	if cluster.Spec.Resources != nil {
		resources = cluster.Spec.Resources
	}
	if cluster.Spec.Storage != nil {
		storage = cluster.Spec.Storage
	}
	if cluster.Spec.PostgreSQLConfig != nil {
		engineConfig = cluster.Spec.PostgreSQLConfig
	}
	if cluster.Spec.PgHBA != nil {
		pgHBA = cluster.Spec.PgHBA
	}

	return &enterprisev4.ClusterSpec{
		Instances:        instances,
		PostgresVersion:  postgresVersion,
		Resources:        resources,
		Storage:          storage,
		PostgreSQLConfig: engineConfig,
		PgHBA:            pgHBA,
	}

}

// defineNewCNPGCluster defines a new CNPG Cluster resource based on the merged configuration.
func (r *ClusterReconciler) defineNewCNPGCluster(
	clusterClass *enterprisev4.ClusterClass,
	cluster *enterprisev4.Cluster,
	mergedConfig *enterprisev4.ClusterSpec,
) (*cnpgv1.Cluster, error) {

	// Validate that required fields are present in the merged configuration before creating the CNPG Cluster.
	if err := validateClusterConfig(mergedConfig, clusterClass); err != nil {
		return nil, err
	}

	resources := corev1.ResourceRequirements{}
	if mergedConfig.Resources != nil {
		resources = *mergedConfig.Resources
	}

	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *mergedConfig.PostgresVersion),
			Instances: int(*mergedConfig.Instances),
			PostgresConfiguration: cnpgv1.PostgresConfiguration{
				Parameters: mergedConfig.PostgreSQLConfig,
				PgHBA:      mergedConfig.PgHBA,
			},
			Bootstrap: &cnpgv1.BootstrapConfiguration{
				InitDB: &cnpgv1.BootstrapInitDB{
					Database: "postgres",
					Owner:    "postgres",
				},
			},
			StorageConfiguration: cnpgv1.StorageConfiguration{
				Size: mergedConfig.Storage.String(),
			},
			Resources: resources,
		},
	}

	if err := ctrl.SetControllerReference(cluster, cnpgCluster, r.Scheme); err != nil {
		return nil, err
	}
	return cnpgCluster, nil
}

// ensureClusterUpToDate ensures that the CNPG Cluster matches the desired state based on the merged configuration.
func (r *ClusterReconciler) ensureClusterUpToDate(
	ctx context.Context,
	clusterClass *enterprisev4.ClusterClass,
	cnpgCluster *cnpgv1.Cluster,
	mergedConfig *enterprisev4.ClusterSpec,
) (bool, error) {

	// Validate that required fields are present in the merged configuration before updating the CNPG Cluster.
	if err := validateClusterConfig(mergedConfig, clusterClass); err != nil {
		return false, err
	}

	resources := corev1.ResourceRequirements{}
	if mergedConfig.Resources != nil {
		resources = *mergedConfig.Resources
	}

	desiredState := cnpgv1.ClusterSpec{
		ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *mergedConfig.PostgresVersion),
		Instances: int(*mergedConfig.Instances),
		PostgresConfiguration: cnpgv1.PostgresConfiguration{
			Parameters: mergedConfig.PostgreSQLConfig,
			PgHBA:      mergedConfig.PgHBA,
		},
		StorageConfiguration: cnpgv1.StorageConfiguration{
			Size: mergedConfig.Storage.String(),
		},
		Resources: resources,
	}

	if !equality.Semantic.DeepEqual(cnpgCluster.Spec, desiredState) {
		cnpgCluster.Spec = desiredState
		if err := r.Update(ctx, cnpgCluster); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// updateClusterStatus updates the status of the Cluster resource based on the status of the CNPG Cluster.
func (r *ClusterReconciler) updateClusterStatus(ctx context.Context, cluster *enterprisev4.Cluster, cnpgCluster *cnpgv1.Cluster) error {
	switch cnpgCluster.Status.Phase {
	case cnpgv1.PhaseHealthy:
		cluster.Status.Phase = "Ready"
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "ClusterHealthy",
			Message:            "CNPG cluster is in healthy state",
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: cluster.CreationTimestamp,
		})
	case cnpgv1.PhaseUnrecoverable:
		cluster.Status.Phase = "Failed"
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ClusterCreationFailed",
			Message:            "CNPG cluster is in unrecoverable state",
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: cluster.CreationTimestamp,
		})
	case "":
		cluster.Status.Phase = "Pending"
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ClusterPending",
			Message:            "CNPG cluster is pending creation",
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: cluster.CreationTimestamp,
		})
	default:
		cluster.Status.Phase = "Provisioning"
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ClusterProvisioning",
			Message:            "CNPG cluster is being provisioned",
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: cluster.CreationTimestamp,
		})
	}

	cluster.Status.ProvisionerRef = &corev1.ObjectReference{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Cluster",
		Namespace:  cnpgCluster.Namespace,
		Name:       cnpgCluster.Name,
		UID:        cnpgCluster.UID,
	}
	if err := r.Status().Update(ctx, cluster); err != nil {
		return err
	}
	return nil
}

// validateClusterConfig checks that all required fields are present in the merged configuration.
func validateClusterConfig(mergedConfig *enterprisev4.ClusterSpec, clusterClass *enterprisev4.ClusterClass) error {
	if mergedConfig == nil {
		return fmt.Errorf("mergedConfig is nil")
	}
	if clusterClass == nil {
		return fmt.Errorf("clusterClass is nil")
	}
	cfg := mergedConfig

	switch {
	case cfg.Instances == nil:
		return fmt.Errorf("missing required field in merged configuration: Instances")
	case cfg.Storage == nil:
		return fmt.Errorf("missing required field in merged configuration: Storage")
	case cfg.PostgresVersion == nil:
		return fmt.Errorf("missing required field in merged configuration: PostgresVersion")
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.Cluster{}).
		Owns(&cnpgv1.Cluster{}).
		Named("cluster").
		Complete(r)
}
