package enterprise

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

func EnsureDatabaseForSHC(ctx context.Context, c client.Client, shc *v4.SearchHeadCluster) error {
	ns, name := shc.Namespace, shc.Name
	mode := v4.DatabaseModeAuto
	if shc.Spec.Database != nil && shc.Spec.Database.Mode != "" {
		mode = shc.Spec.Database.Mode
	}

	switch mode {
	case v4.DatabaseModeExternal:
		ensureSummaryExternal(shc)
		return nil

	case v4.DatabaseModeManagedRef:
		if shc.Spec.Database == nil || shc.Spec.Database.ManagedRef == nil || shc.Spec.Database.ManagedRef.Name == "" {
			return nil
		}
		var db v4.DatabaseCluster
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: shc.Spec.Database.ManagedRef.Name}, &db); err == nil {
			fromDB(&db, shc)
		}
		return nil

	default: // Auto → create a DatabaseClaim with proper ownership
		claimName := ternStr(shc.Spec.Database != nil && shc.Spec.Database.AutoNameSuffix != "",
			fmt.Sprintf("%s-%s", name, shc.Spec.Database.AutoNameSuffix), name+"-db")

		className := "cnpg-standard"
		if shc.Spec.Database != nil && shc.Spec.Database.DatabaseClassName != "" {
			className = shc.Spec.Database.DatabaseClassName
		}

		// Create or update DatabaseClaim with proper owner reference
		desired := v4.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: ns,
			},
			Spec: v4.DatabaseClaimSpec{
				ClassName:            className,
				ConnectionSecretName: claimName + "-conn",
				ReclaimPolicy:        v4.ReclaimDelete,
			},
		}

		// Add S3 bucket override if specified
		if shc.Spec.Database != nil && shc.Spec.Database.S3BackupBucket != "" {
			desired.Spec.Overrides = &v4.DatabaseClaimOverrides{
				BackupConfig: &v4.BackupOverride{
					S3: &v4.S3Override{
						Bucket: shc.Spec.Database.S3BackupBucket,
						Path:   fmt.Sprintf("shc/%s", shc.Name), // Use convention: shc/<name>
					},
				},
			}
		}

		var existing v4.DatabaseClaim
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: claimName}, &existing); err != nil {
			// DatabaseClaim doesn't exist - create it with owner reference
			if err := controllerutil.SetControllerReference(shc, &desired, c.Scheme()); err != nil {
				return fmt.Errorf("failed to set controller reference on DatabaseClaim: %w", err)
			}

			// Add labels for traceability
			if desired.Labels == nil {
				desired.Labels = map[string]string{}
			}
			desired.Labels["app.kubernetes.io/managed-by"] = "splunk-operator"
			desired.Labels["database.splunk.com/owner-shc"] = shc.Name

			if err := c.Create(ctx, &desired); err != nil {
				return fmt.Errorf("failed to create DatabaseClaim: %w", err)
			}
		} else {
			// DatabaseClaim exists - ensure owner reference is set
			if !hasOwnerReference(&existing, shc) {
				if err := controllerutil.SetControllerReference(shc, &existing, c.Scheme()); err != nil {
					return fmt.Errorf("failed to set controller reference on existing DatabaseClaim: %w", err)
				}
				if err := c.Update(ctx, &existing); err != nil {
					return fmt.Errorf("failed to update DatabaseClaim with owner reference: %w", err)
				}
			}
		}

		// Try to summarize if the bound cluster exists
		var db v4.DatabaseCluster
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: claimName}, &db); err == nil {
			fromDB(&db, shc)
		}
		return nil
	}
}

func ensureSummaryExternal(shc *v4.SearchHeadCluster) {
	if shc.Status.DatabaseSummary == nil {
		shc.Status.DatabaseSummary = &v4.DatabaseSummary{}
	}
	shc.Status.DatabaseSummary.Provider = "External"
	shc.Status.DatabaseSummary.Phase = "Ready"
}

func fromDB(db *v4.DatabaseCluster, shc *v4.SearchHeadCluster) {
	if shc.Status.DatabaseSummary == nil {
		shc.Status.DatabaseSummary = &v4.DatabaseSummary{}
	}
	shc.Status.DatabaseSummary.Provider = db.Status.Provider
	shc.Status.DatabaseSummary.Phase = db.Status.Phase
	if db.Status.Endpoints != nil {
		shc.Status.DatabaseSummary.Endpoint = db.Status.Endpoints.ReadWrite
	}
	shc.Status.DatabaseSummary.Version = db.Status.Version
	shc.Status.DatabaseSummary.LastBackup = db.Status.LastBackupTime
}

func hasOwnerReference(obj *v4.DatabaseClaim, owner *v4.SearchHeadCluster) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.UID {
			return true
		}
	}
	return false
}

func ternStr(b bool, x, y string) string {
	if b {
		return x
	}
	return y
}
