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
	"errors"
	"fmt"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	logs "sigs.k8s.io/controller-runtime/pkg/log"
)

// PostgresClusterReconciler reconciles a PostgresCluster object
type PostgresClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// This struct is used to compare the merged configuration from PostgresClusterClass and PostgresClusterSpec
// in a normalized way, and not to use CNPG-default values which are causing false positive diff state while reconciliation loop.
// It contains only the fields that are relevant for our reconciliation and that we want to compare when deciding whether to update the CNPG Cluster spec or not.
type normalizedCNPGClusterSpec struct {
	ImageName string
	Instances int
	// Parameters we set, instead of complete spec from CNPG
	CustomDefinedParameters map[string]string
	PgHBA                   []string
	DefaultDatabase         string
	Owner                   string
	StorageSize             string
	Resources               corev1.ResourceRequirements
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusterclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get

// Main reconciliation loop for PostgresCluster.
func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	logger := logs.FromContext(ctx)
	logger.Info("Reconciling PostgresCluster", "name", req.Name, "namespace", req.Namespace)

	// Initialize as nil so syncStatus knows if the object was actually found/created.
	var cnpgCluster *cnpgv1.Cluster

	// 1. Fetch the PostgresCluster instance, stop, if not found.
	postgresCluster := &enterprisev4.PostgresCluster{}
	if getPGClusterErr := r.Get(ctx, req.NamespacedName, postgresCluster); getPGClusterErr != nil {
		if apierrors.IsNotFound(getPGClusterErr) {
			logger.Info("PostgresCluster deleted, skipping reconciliation")
			return res, nil
		}
		logger.Error(getPGClusterErr, "Unable to fetch PostgresCluster")
		return res, getPGClusterErr
	}

	// This deferred function will run at the end of the reconciliation process, regardless of whether it exits early due to an error or completes successfully.
	// It ensures that we always attempt to sync the status of the PostgresCluster based on the final state of the CNPG Cluster and any errors that may have occurred.
	defer func() {
		if syncErr := r.syncStatus(ctx, postgresCluster, cnpgCluster, err); syncErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to sync status in deferred function: %w", syncErr))
		}
	}()

	// 2. Load the referenced PostgresClusterClass.
	postgresClusterClass := &enterprisev4.PostgresClusterClass{}
	if getClusterClassErr := r.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, postgresClusterClass); getClusterClassErr != nil {
		logger.Error(getClusterClassErr, "Unable to fetch referenced PostgresClusterClass", "className", postgresCluster.Spec.Class)
		r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterClassNotFound", getClusterClassErr.Error())
		return res, getClusterClassErr
	}

	// 3. Create the merged configuration by overlaying PostgresClusterSpec on top of PostgresClusterClass defaults.
	mergedConfig := r.getMergedConfig(postgresClusterClass, postgresCluster)

	// 4. Build the desired CNPG Cluster spec based on the merged configuration.
	desiredSpec := r.buildCNPGClusterSpec(mergedConfig)

	// 5. Fetch existing CNPG Cluster or create it if it doesn't exist yet.
	existingCNPG := &cnpgv1.Cluster{}
	err = r.Get(ctx, types.NamespacedName{Name: postgresCluster.Name, Namespace: postgresCluster.Namespace}, existingCNPG)
	switch {
	case apierrors.IsNotFound(err):
		logger.Info("CNPG Cluster not found, creating", "name", postgresCluster.Name)
		newCluster := r.buildCNPGCluster(postgresCluster, mergedConfig)
		if err = r.Create(ctx, newCluster); err != nil {
			logger.Error(err, "Failed to create CNPG Cluster")
			r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterBuildFailed", err.Error())
			return res, err
		}
		logger.Info("CNPG Cluster created successfully, requeueing for status update", "name", postgresCluster.Name)
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	case err != nil:
		logger.Error(err, "Failed to get CNPG Cluster")
		r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterGetFailed", err.Error())
		return res, err
	}
	cnpgCluster = existingCNPG

	// 6. If CNPG Cluster exists, compare the current spec with the desired spec and update if necessary.
	currentNormalizedSpec := normalizeCNPGClusterSpec(cnpgCluster.Spec, mergedConfig.PostgreSQLConfig)
	desiredNormalizedSpec := normalizeCNPGClusterSpec(desiredSpec, mergedConfig.PostgreSQLConfig)
	if !equality.Semantic.DeepEqual(currentNormalizedSpec, desiredNormalizedSpec) {
		originalCluster := cnpgCluster.DeepCopy()
		cnpgCluster.Spec = desiredSpec
		if patchCNPGClusterErr := r.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); patchCNPGClusterErr != nil {
			if apierrors.IsConflict(patchCNPGClusterErr) {
				logger.Info("Conflict occurred while updating CNPG Cluster, requeueing", "name", cnpgCluster.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(patchCNPGClusterErr, "Failed to patch CNPG Cluster", "name", cnpgCluster.Name)
			r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterUpdateFailed", patchCNPGClusterErr.Error())
			return res, patchCNPGClusterErr
		}
		logger.Info("CNPG Cluster patched successfully, requeueing for status update", "name", cnpgCluster.Name)
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	// 7. Reconcile ManagedRoles from PostgresCluster to CNPG Cluster
	if err := r.reconcileManagedRoles(ctx, postgresCluster, cnpgCluster); err != nil {
		logger.Error(err, "Failed to reconcile managed roles")
		r.setCondition(postgresCluster, metav1.ConditionFalse, "ManagedRolesFailed", err.Error())
		return res, err
	}

	// 8. Report progress back to the user and manage the reconciliation lifecycle.
	logger.Info("Reconciliation completed successfully", "name", postgresCluster.Name)
	r.setCondition(postgresCluster, metav1.ConditionTrue, "ClusterUpdateSucceeded", fmt.Sprintf("Reconciliation completed successfully: %s", postgresCluster.Name))
	return res, nil
}

// getMergedConfig merges the configuration from the PostgresClusterClass into the PostgresClusterSpec, giving precedence to the PostgresClusterSpec values.
func (r *PostgresClusterReconciler) getMergedConfig(clusterClass *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) *enterprisev4.PostgresClusterSpec {
	resultConfig := cluster.Spec.DeepCopy()
	classDefaults := clusterClass.Spec.Config

	if resultConfig.Instances == nil {
		resultConfig.Instances = classDefaults.Instances
	}
	if resultConfig.PostgresVersion == nil {
		resultConfig.PostgresVersion = classDefaults.PostgresVersion
	}
	if resultConfig.Resources == nil {
		resultConfig.Resources = classDefaults.Resources
	}
	if resultConfig.Storage == nil {
		resultConfig.Storage = classDefaults.Storage
	}
	if len(resultConfig.PostgreSQLConfig) == 0 {
		resultConfig.PostgreSQLConfig = classDefaults.PostgreSQLConfig
	}
	if len(resultConfig.PgHBA) == 0 {
		resultConfig.PgHBA = classDefaults.PgHBA
	}

	// Ensure that maps and slices are initialized to empty if they are still nil after merging, to prevent potential nil pointer dereferences later on.
	if resultConfig.PostgreSQLConfig == nil {
		resultConfig.PostgreSQLConfig = make(map[string]string)
	}
	if resultConfig.PgHBA == nil {
		resultConfig.PgHBA = make([]string, 0)
	}
	// Ensure that Resources is initialized to an empty struct if it's still nil after merging, to prevent potential nil pointer dereferences later on.
	if resultConfig.Resources == nil {
		resultConfig.Resources = &corev1.ResourceRequirements{}
	}

	return resultConfig
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec.
// IMPORTANT: any field added here must also be added to normalizedCNPGClusterSpec and normalizeCNPGClusterSpec,
// otherwise it will not be included in drift detection and changes will be silently ignored.
func (r *PostgresClusterReconciler) buildCNPGClusterSpec(mergedConfig *enterprisev4.PostgresClusterSpec) cnpgv1.ClusterSpec {

	// 3. Build the Spec
	spec := cnpgv1.ClusterSpec{
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
		Resources: *mergedConfig.Resources,
	}

	return spec
}

// build CNPGCluster builds the CNPG Cluster object based on the PostgresCluster resource and merged configuration.
func (r *PostgresClusterReconciler) buildCNPGCluster(postgresCluster *enterprisev4.PostgresCluster, mergedConfig *enterprisev4.PostgresClusterSpec) *cnpgv1.Cluster {

	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresCluster.Name,
			Namespace: postgresCluster.Namespace,
		},
		Spec: r.buildCNPGClusterSpec(mergedConfig),
	}

	ctrl.SetControllerReference(postgresCluster, cnpgCluster, r.Scheme)
	return cnpgCluster
}

// syncStatus maps CNPG Cluster state to PostgresCluster object.
func (r *PostgresClusterReconciler) syncStatus(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, err error) error {
	// will use Patch as we did for main reconciliation loop.
	latestPGCluster := client.MergeFrom(postgresCluster.DeepCopy())

	// If there's an error, we set the status to Error and include the error message in the condition.
	if err != nil {
		postgresCluster.Status.Phase = "Error"
		r.setCondition(postgresCluster, metav1.ConditionFalse, "Error", fmt.Sprintf("Error during reconciliation: %v", err))
		// CNPG not existing, set status to Pending. Direct running `switch` without CNPG cluster in place will cause a panic, so we need to check for that first.
	} else if cnpgCluster == nil {
		postgresCluster.Status.Phase = "Pending"
		r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterNotFound", "Underlying CNPG cluster object has not been created yet")
		// Cluster exists, map the CNPG Cluster status to our PostgresCluster status.
	} else {
		switch cnpgCluster.Status.Phase {
		case cnpgv1.PhaseHealthy:
			postgresCluster.Status.Phase = "Ready"
			r.setCondition(postgresCluster, metav1.ConditionTrue, "ClusterHealthy", "CNPG cluster is in healthy state")
		case cnpgv1.PhaseUnrecoverable:
			postgresCluster.Status.Phase = "Failed"
			r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterCreationFailed", "CNPG cluster is in unrecoverable state")
		case "":
			postgresCluster.Status.Phase = "Pending"
			r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterPending", "CNPG cluster is pending creation")
		default:
			postgresCluster.Status.Phase = "Provisioning"
			r.setCondition(postgresCluster, metav1.ConditionFalse, "ClusterProvisioning", "CNPG cluster is being provisioned")
		}
		// Set the reference to the CNPG Cluster in the status.
		postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: "postgresql.cnpg.io/v1",
			Kind:       "Cluster",
			Namespace:  cnpgCluster.Namespace,
			Name:       cnpgCluster.Name,
			UID:        cnpgCluster.UID,
		}
	}

	if patchErr := r.Status().Patch(ctx, postgresCluster, latestPGCluster); patchErr != nil {
		return fmt.Errorf("failed to patch PostgresCluster status: %w", patchErr)
	}
	return nil
}

// setCondition sets the condition of the PostgresCluster status.
func (r *PostgresClusterReconciler) setCondition(postgresCluster *enterprisev4.PostgresCluster, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&postgresCluster.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: postgresCluster.Generation,
	})
}

// reconcileManagedRoles synchronizes ManagedRoles from PostgresCluster spec to CNPG Cluster managed.roles using diff-based patching
func (r *PostgresClusterReconciler) reconcileManagedRoles(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := logs.FromContext(ctx)

	// If no managed roles in PostgresCluster spec, nothing to do for now
	// TODO: Should we remove roles from CNPG if they're removed from PostgresCluster?
	if len(postgresCluster.Spec.ManagedRoles) == 0 {
		logger.Info("No managed roles to reconcile")
		return nil
	}

	// Convert PostgresCluster ManagedRoles to CNPG RoleConfiguration format
	desiredRoles := []cnpgv1.RoleConfiguration{}
	for _, role := range postgresCluster.Spec.ManagedRoles {
		cnpgRole := cnpgv1.RoleConfiguration{
			Name: role.Name,
		}

		if role.Ensure == "absent" {
			cnpgRole.Ensure = cnpgv1.EnsureAbsent
		} else {
			cnpgRole.Ensure = cnpgv1.EnsurePresent
		}

		if role.PasswordSecretRef != nil {
			cnpgRole.PasswordSecret = &cnpgv1.LocalObjectReference{
				Name: role.PasswordSecretRef.Name,
			}
		}

		desiredRoles = append(desiredRoles, cnpgRole)
	}

	var currentRoles []cnpgv1.RoleConfiguration
	if cnpgCluster.Spec.Managed != nil && cnpgCluster.Spec.Managed.Roles != nil {
		currentRoles = cnpgCluster.Spec.Managed.Roles
	}

	if equality.Semantic.DeepEqual(currentRoles, desiredRoles) {
		logger.Info("CNPG Cluster roles already match desired state, no update needed")
		return nil
	}

	logger.Info("CNPG Cluster roles differ from desired state, updating",
		"currentCount", len(currentRoles),
		"desiredCount", len(desiredRoles))

	originalCluster := cnpgCluster.DeepCopy()

	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desiredRoles

	if err := r.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); err != nil {
		return fmt.Errorf("failed to patch CNPG Cluster with managed roles: %w", err)
	}

	logger.Info("Successfully updated CNPG Cluster with managed roles", "roleCount", len(desiredRoles))
	return nil
}

func normalizeCNPGClusterSpec(spec cnpgv1.ClusterSpec, customDefinedParameters map[string]string) normalizedCNPGClusterSpec {
	normalizedConf := normalizedCNPGClusterSpec{
		ImageName: spec.ImageName,
		Instances: spec.Instances,
		// Parameters intentionally excluded — CNPG injects defaults that we don't change
		StorageSize: spec.StorageConfiguration.Size,
		Resources:   spec.Resources,
	}

	if len(customDefinedParameters) > 0 {
		normalizedConf.CustomDefinedParameters = make(map[string]string)
		for k := range customDefinedParameters {
			normalizedConf.CustomDefinedParameters[k] = spec.PostgresConfiguration.Parameters[k]
		}
	}
	if len(spec.PostgresConfiguration.PgHBA) > 0 {
		normalizedConf.PgHBA = spec.PostgresConfiguration.PgHBA
	}

	if spec.Bootstrap != nil && spec.Bootstrap.InitDB != nil {
		normalizedConf.DefaultDatabase = spec.Bootstrap.InitDB.Database
		normalizedConf.Owner = spec.Bootstrap.InitDB.Owner
	}
	return normalizedConf
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresCluster{}).
		Owns(&cnpgv1.Cluster{}).
		Named("postgresCluster").
		Complete(r)
}
