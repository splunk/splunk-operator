package claimbinder

import (
	"context"
	"fmt"

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

	// handle deletion first
	if !claim.DeletionTimestamp.IsZero() {
		return finalizeClaim(ctx, c, claim)
	}
	if !controllerutil.ContainsFinalizer(claim, claimFinalizer) {
		controllerutil.AddFinalizer(claim, claimFinalizer)
		return c.Update(ctx, claim)
	}

	// 1) Fetch class
	var class v4.DatabaseClass
	if err := c.Get(ctx, types.NamespacedName{Name: claim.Spec.ClassName}, &class); err != nil {
		setClaimPhase(claim, v4.ClaimPhasePending, "ClassNotFound", fmt.Sprintf("DatabaseClass %q not found", claim.Spec.ClassName))
		return nil
	}

	// 2) Desired DatabaseCluster from class + params
	desired := v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name, // bind 1:1: claim name == cluster name
			Namespace: ns,
		},
		Spec: v4.DatabaseClusterSpec{
			Engine:        "Postgres",
			Version:       class.Spec.Version,
			ManagedBy:     tern(class.Spec.ManagedBy != "", class.Spec.ManagedBy, "CNPG"),
			Connection:    &v4.ConnectionSpec{SecretName: ternStr(claim.Spec.ConnectionSecretName != "", claim.Spec.ConnectionSecretName, name+"-conn")},
			CNPG:          class.Spec.CNPG,
			Backup:        class.Spec.Backup,
			ReclaimPolicy: string(claim.Spec.ReclaimPolicy),
		},
	}

	// 3) Create or update DBCluster and ownerRef it to the Claim
	var db v4.DatabaseCluster
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &db); err != nil {
		if err := ctrl.SetControllerReference(claim, &desired, scheme); err == nil {
			_ = c.Create(ctx, &desired)
		}
	} else {
		changed := false
		if db.Spec.ManagedBy != desired.Spec.ManagedBy {
			db.Spec.ManagedBy = desired.Spec.ManagedBy
			changed = true
		}
		if db.Spec.Version != desired.Spec.Version {
			db.Spec.Version = desired.Spec.Version
			changed = true
		}
		if db.Spec.CNPG != nil && desired.Spec.CNPG != nil {
			if db.Spec.CNPG.Instances != desired.Spec.CNPG.Instances {
				db.Spec.CNPG.Instances = desired.Spec.CNPG.Instances
				changed = true
			}
			if db.Spec.CNPG.Storage.Size != desired.Spec.CNPG.Storage.Size {
				db.Spec.CNPG.Storage.Size = desired.Spec.CNPG.Storage.Size
				changed = true
			}
			if db.Spec.CNPG.Storage.StorageClassName != desired.Spec.CNPG.Storage.StorageClassName {
				db.Spec.CNPG.Storage.StorageClassName = desired.Spec.CNPG.Storage.StorageClassName
				changed = true
			}
		}
		if changed {
			_ = c.Update(ctx, &db)
		}
	}

	// 4) Mark Bound
	claim.Status.BoundRef = &v4.LocalRef{Name: name}
	setClaimPhase(claim, v4.ClaimPhaseBound, "Bound", "DatabaseCluster bound")
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
	if claim.Status.Conditions == nil {
		claim.Status.Conditions = []metav1.Condition{cond}
	} else {
		// Optional: replace existing "Bound" condition instead of appending endlessly
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
}

func tern[T any](b bool, x, y T) T {
	if b {
		return x
	}
	return y
}
func ternStr(b bool, x, y string) string {
	if b {
		return x
	}
	return y
}

func finalizeClaim(ctx context.Context, c client.Client, claim *v4.DatabaseClaim) error {
	// If we know which cluster we bound, apply reclaim policy
	var name = claim.Name
	if claim.Status.BoundRef != nil && claim.Status.BoundRef.Name != "" {
		name = claim.Status.BoundRef.Name
	}

	switch claim.Spec.ReclaimPolicy {
	case v4.ReclaimDelete, "":
		// delete the DatabaseCluster (this will GC provider children via ownerRef on the cluster)
		var db v4.DatabaseCluster
		if err := c.Get(ctx, types.NamespacedName{Namespace: claim.Namespace, Name: name}, &db); err == nil {
			_ = c.Delete(ctx, &db)
		}
	case v4.ReclaimRetain:
		// retain: do nothing; the DatabaseCluster stays for adoption or manual cleanup
	}

	controllerutil.RemoveFinalizer(claim, claimFinalizer)
	return c.Update(ctx, claim)
}
