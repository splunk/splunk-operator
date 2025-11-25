package claimbinder

import (
	"context"
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

const claimFinalizer = "database.splunk.com/claim-protect"

func ReconcileDatabaseClaim(ctx context.Context, c client.Client, scheme *runtime.Scheme, claim *v4.DatabaseClaim) error {
	ns, name := claim.Namespace, claim.Name

	// Handle deletion first
	if !claim.DeletionTimestamp.IsZero() {
		return finalizeClaim(ctx, c, claim)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(claim, claimFinalizer) {
		controllerutil.AddFinalizer(claim, claimFinalizer)
		if err := c.Update(ctx, claim); err != nil {
			return fmt.Errorf("failed to add finalizer: %w", err)
		}
		return nil
	}

	// 1) Fetch class
	var class v4.DatabaseClass
	if err := c.Get(ctx, types.NamespacedName{Name: claim.Spec.ClassName}, &class); err != nil {
		setClaimPhase(claim, v4.ClaimPhasePending, "ClassNotFound", fmt.Sprintf("DatabaseClass %q not found", claim.Spec.ClassName))
		return nil
	}

	// 2) Synthesize desired DatabaseCluster from class + claim parameters
	desired, err := synthesizeCluster(claim, &class)
	if err != nil {
		setClaimPhase(claim, v4.ClaimPhasePending, "SynthesisError", err.Error())
		return err
	}

	// 3) Create or update DatabaseCluster and set owner reference
	var db v4.DatabaseCluster
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &db); err != nil {
		// DatabaseCluster doesn't exist - create it
		if err := ctrl.SetControllerReference(claim, &desired, scheme); err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}
		if err := c.Create(ctx, &desired); err != nil {
			return fmt.Errorf("failed to create DatabaseCluster: %w", err)
		}
	} else {
		// DatabaseCluster exists - update if needed
		changed := false

		// Update provider config
		if db.Spec.Provider.Type != desired.Spec.Provider.Type {
			db.Spec.Provider.Type = desired.Spec.Provider.Type
			changed = true
		}

		// Update CNPG-specific config if applicable
		if db.Spec.Provider.CNPG != nil && desired.Spec.Provider.CNPG != nil {
			if db.Spec.Provider.CNPG.Instances != desired.Spec.Provider.CNPG.Instances {
				db.Spec.Provider.CNPG.Instances = desired.Spec.Provider.CNPG.Instances
				changed = true
			}
			if db.Spec.Provider.CNPG.Storage.Size != desired.Spec.Provider.CNPG.Storage.Size {
				db.Spec.Provider.CNPG.Storage.Size = desired.Spec.Provider.CNPG.Storage.Size
				changed = true
			}
			if db.Spec.Provider.CNPG.Storage.StorageClassName != desired.Spec.Provider.CNPG.Storage.StorageClassName {
				db.Spec.Provider.CNPG.Storage.StorageClassName = desired.Spec.Provider.CNPG.Storage.StorageClassName
				changed = true
			}
		}

		// Update requirements
		if desired.Spec.Requirements != nil {
			if db.Spec.Requirements == nil {
				db.Spec.Requirements = desired.Spec.Requirements
				changed = true
			} else {
				if db.Spec.Requirements.MinMemory != desired.Spec.Requirements.MinMemory {
					db.Spec.Requirements.MinMemory = desired.Spec.Requirements.MinMemory
					changed = true
				}
				if db.Spec.Requirements.MinStorageGB != desired.Spec.Requirements.MinStorageGB {
					db.Spec.Requirements.MinStorageGB = desired.Spec.Requirements.MinStorageGB
					changed = true
				}
			}
		}

		if changed {
			if err := c.Update(ctx, &db); err != nil {
				return fmt.Errorf("failed to update DatabaseCluster: %w", err)
			}
		}
	}

	// 4) Mark claim as Bound
	claim.Status.BoundRef = &v4.LocalRef{Name: name}
	setClaimPhase(claim, v4.ClaimPhaseBound, "Bound", "DatabaseCluster successfully bound")
	return nil
}

func synthesizeCluster(claim *v4.DatabaseClaim, class *v4.DatabaseClass) (v4.DatabaseCluster, error) {
	// Start with class defaults
	cluster := v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claim.Name,
			Namespace: claim.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "splunk-operator",
				"database.splunk.com/claim":    claim.Name,
				"database.splunk.com/class":    claim.Spec.ClassName,
			},
		},
		Spec: v4.DatabaseClusterSpec{
			Engine:        tern(class.Spec.Engine != "", class.Spec.Engine, "Postgres"),
			Provider:      class.Spec.Provider,
			Requirements:  class.Spec.Requirements,
			Backup:        class.Spec.Backup,
			ReclaimPolicy: string(claim.Spec.ReclaimPolicy),
			Connection: &v4.ConnectionSpec{
				SecretName: tern(claim.Spec.ConnectionSecretName != "", claim.Spec.ConnectionSecretName, claim.Name+"-conn"),
			},
		},
	}

	// Apply claim parameter overrides
	if len(claim.Spec.Parameters) > 0 {
		if err := applyParameterOverrides(&cluster, class, claim.Spec.Parameters); err != nil {
			return cluster, fmt.Errorf("failed to apply parameter overrides: %w", err)
		}
	}

	// Apply structural overrides (S3 backup bucket, etc.)
	if claim.Spec.Overrides != nil {
		applyStructuralOverrides(&cluster, claim.Spec.Overrides)
	}

	return cluster, nil
}

func applyParameterOverrides(cluster *v4.DatabaseCluster, class *v4.DatabaseClass, params map[string]string) error {
	// Check if parameter overrides are allowed by class policy
	allowedOverrides := make(map[string]bool)
	if class.Spec.Policy != nil && len(class.Spec.Policy.AllowInlineOverrides) > 0 {
		for _, override := range class.Spec.Policy.AllowInlineOverrides {
			allowedOverrides[override] = true
		}
	} else {
		// Default: allow common safe overrides
		allowedOverrides["minStorageGB"] = true
		allowedOverrides["minMemory"] = true
		allowedOverrides["cnpg.storageSize"] = true
		allowedOverrides["cnpg.instances"] = true
	}

	// Apply allowed overrides
	for key, value := range params {
		if !allowedOverrides[key] {
			return fmt.Errorf("parameter override %q is not allowed by class policy", key)
		}

		switch key {
		case "minStorageGB":
			if cluster.Spec.Requirements == nil {
				cluster.Spec.Requirements = &v4.DatabaseRequirements{}
			}
			val, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid minStorageGB value %q: %w", value, err)
			}
			cluster.Spec.Requirements.MinStorageGB = val

		case "minMemory":
			if cluster.Spec.Requirements == nil {
				cluster.Spec.Requirements = &v4.DatabaseRequirements{}
			}
			cluster.Spec.Requirements.MinMemory = value

		case "cnpg.storageSize":
			if cluster.Spec.Provider.CNPG != nil {
				cluster.Spec.Provider.CNPG.Storage.Size = value
			}

		case "cnpg.instances":
			if cluster.Spec.Provider.CNPG != nil {
				val, err := strconv.ParseInt(value, 10, 32)
				if err != nil {
					return fmt.Errorf("invalid cnpg.instances value %q: %w", value, err)
				}
				cluster.Spec.Provider.CNPG.Instances = int32(val)
			}
		}
	}

	return nil
}

func setClaimPhase(claim *v4.DatabaseClaim, phase v4.ClaimPhase, reason, msg string) {
	claim.Status.Phase = phase
	cond := metav1.Condition{
		Type:               "Bound",
		Status:             tern(phase == v4.ClaimPhaseBound, metav1.ConditionTrue, metav1.ConditionFalse),
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}

	// Replace existing "Bound" condition or append new one
	replaced := false
	for i := range claim.Status.Conditions {
		if claim.Status.Conditions[i].Type == cond.Type {
			claim.Status.Conditions[i] = cond
			replaced = true
			break
		}
	}
	if !replaced {
		claim.Status.Conditions = append(claim.Status.Conditions, cond)
	}
}

func finalizeClaim(ctx context.Context, c client.Client, claim *v4.DatabaseClaim) error {
	if !controllerutil.ContainsFinalizer(claim, claimFinalizer) {
		return nil
	}

	// Determine which cluster to delete
	clusterName := claim.Name
	if claim.Status.BoundRef != nil && claim.Status.BoundRef.Name != "" {
		clusterName = claim.Status.BoundRef.Name
	}

	// Handle reclaim policy
	switch claim.Spec.ReclaimPolicy {
	case v4.ReclaimDelete, "":
		// Delete the DatabaseCluster (cascade deletion via owner references)
		var db v4.DatabaseCluster
		if err := c.Get(ctx, types.NamespacedName{Namespace: claim.Namespace, Name: clusterName}, &db); err == nil {
			if err := c.Delete(ctx, &db); err != nil {
				return fmt.Errorf("failed to delete DatabaseCluster: %w", err)
			}
		}

	case v4.ReclaimRetain:
		// Leave the DatabaseCluster and connection secret intact
		// Update conditions to warn that it's orphaned
		setClaimPhase(claim, v4.ClaimPhaseLost, "Retained", "Claim deleted with Retain policy - cluster still running")
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(claim, claimFinalizer)
	return c.Update(ctx, claim)
}

func tern[T any](b bool, x, y T) T {
	if b {
		return x
	}
	return y
}

// applyStructuralOverrides applies high-level overrides from DatabaseClaim.Overrides
// to the synthesized DatabaseCluster. This enables simple APIs like:
// SearchHeadCluster.spec.database.s3BackupBucket → DatabaseClaim.Overrides.BackupConfig.S3.Bucket
func applyStructuralOverrides(cluster *v4.DatabaseCluster, overrides *v4.DatabaseClaimOverrides) {
	if overrides.BackupConfig != nil {
		// Apply backup enabled override
		if overrides.BackupConfig.Enabled != nil {
			if cluster.Spec.Backup == nil {
				cluster.Spec.Backup = &v4.BackupIntent{}
			}
			cluster.Spec.Backup.Enabled = *overrides.BackupConfig.Enabled
		}

		// Apply S3 bucket override (CNPG provider only)
		if overrides.BackupConfig.S3 != nil && cluster.Spec.Provider.CNPG != nil {
			if cluster.Spec.Provider.CNPG.ObjectStore == nil {
				cluster.Spec.Provider.CNPG.ObjectStore = &v4.ObjectStoreSpec{}
			}
			if cluster.Spec.Provider.CNPG.ObjectStore.S3 == nil {
				cluster.Spec.Provider.CNPG.ObjectStore.S3 = &v4.S3Spec{}
			}

			// Override bucket name
			if overrides.BackupConfig.S3.Bucket != "" {
				cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket = overrides.BackupConfig.S3.Bucket
			}

			// Override path if specified
			if overrides.BackupConfig.S3.Path != "" {
				cluster.Spec.Provider.CNPG.ObjectStore.S3.Path = overrides.BackupConfig.S3.Path
			}
		}
	}
}
