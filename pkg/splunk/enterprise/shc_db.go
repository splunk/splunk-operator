package enterprise

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

	default: // Auto → create a DatabaseClaim and let binder produce DatabaseCluster
		claimName := ternStr(shc.Spec.Database != nil && shc.Spec.Database.AutoNameSuffix != "",
			fmt.Sprintf("%s-%s", name, shc.Spec.Database.AutoNameSuffix), name+"-db")

		className := "cnpg-standard"
		if shc.Spec.Database != nil && shc.Spec.Database.DatabaseClassName != "" {
			className = shc.Spec.Database.DatabaseClassName
		}

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
		var existing v4.DatabaseClaim
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: claimName}, &existing); err != nil {
			// Do NOT set ownerRef to SHC; label instead for traceability
			if desired.Labels == nil {
				desired.Labels = map[string]string{}
			}
			desired.Labels["database.splunk.com/owner-shc"] = shc.Name
			_ = c.Create(ctx, &desired)
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

func ternStr(b bool, x, y string) string {
	if b {
		return x
	}
	return y
}
