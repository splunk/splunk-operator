package claimbinder

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

func TestReconcileDatabaseClaim_CreatesCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	// Create a DatabaseClass
	class := &v4.DatabaseClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cnpg-standard",
		},
		Spec: v4.DatabaseClassSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
		},
	}

	// Create a DatabaseClaim
	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-claim",
			Namespace:  "default",
			UID:        "claim-uid",
			Finalizers: []string{claimFinalizer}, // Finalizer needed for cluster creation
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:            "cnpg-standard",
			ConnectionSecretName: "test-conn",
			ReclaimPolicy:        v4.ReclaimDelete,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(class, claim).
		WithStatusSubresource(claim).
		Build()

	ctx := context.Background()
	err := ReconcileDatabaseClaim(ctx, client, scheme, claim)
	if err != nil {
		t.Fatalf("ReconcileDatabaseClaim() failed: %v", err)
	}

	// Verify DatabaseCluster was created
	var cluster v4.DatabaseCluster
	err = client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-claim"}, &cluster)
	if err != nil {
		t.Fatalf("Expected DatabaseCluster to be created, got error: %v", err)
	}

	// Verify cluster spec
	if cluster.Spec.Engine != "Postgres" {
		t.Errorf("Expected engine 'Postgres', got '%s'", cluster.Spec.Engine)
	}
	if cluster.Spec.Provider.Type != "CNPG" {
		t.Errorf("Expected provider type 'CNPG', got '%s'", cluster.Spec.Provider.Type)
	}
	if cluster.Spec.Connection.SecretName != "test-conn" {
		t.Errorf("Expected connection secret 'test-conn', got '%s'", cluster.Spec.Connection.SecretName)
	}

	// Verify owner reference
	if len(cluster.OwnerReferences) != 1 {
		t.Fatalf("Expected 1 owner reference, got %d", len(cluster.OwnerReferences))
	}
	if cluster.OwnerReferences[0].UID != claim.UID {
		t.Errorf("Expected owner UID '%s', got '%s'", claim.UID, cluster.OwnerReferences[0].UID)
	}
	if cluster.OwnerReferences[0].Kind != "DatabaseClaim" {
		t.Errorf("Expected owner kind 'DatabaseClaim', got '%s'", cluster.OwnerReferences[0].Kind)
	}

	// Verify labels
	if cluster.Labels["database.splunk.com/claim"] != "test-claim" {
		t.Error("Expected claim label to be set")
	}
	if cluster.Labels["database.splunk.com/class"] != "cnpg-standard" {
		t.Error("Expected class label to be set")
	}
}

func TestReconcileDatabaseClaim_BindsToCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	class := &v4.DatabaseClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cnpg-standard",
		},
		Spec: v4.DatabaseClassSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
		},
	}

	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-claim",
			Namespace:  "default",
			UID:        "claim-uid",
			Finalizers: []string{claimFinalizer},
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:            "cnpg-standard",
			ConnectionSecretName: "test-conn",
			ReclaimPolicy:        v4.ReclaimDelete,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(class, claim).
		WithStatusSubresource(claim).
		Build()

	ctx := context.Background()
	err := ReconcileDatabaseClaim(ctx, client, scheme, claim)
	if err != nil {
		t.Fatalf("ReconcileDatabaseClaim() failed: %v", err)
	}

	// Verify claim status
	if claim.Status.Phase != v4.ClaimPhaseBound {
		t.Errorf("Expected phase 'Bound', got '%s'", claim.Status.Phase)
	}
	if claim.Status.BoundRef == nil || claim.Status.BoundRef.Name != "test-claim" {
		t.Error("Expected bound reference to be set")
	}

	// Verify condition
	if len(claim.Status.Conditions) == 0 {
		t.Fatal("Expected conditions to be set")
	}
	var boundCondition *metav1.Condition
	for i := range claim.Status.Conditions {
		if claim.Status.Conditions[i].Type == "Bound" {
			boundCondition = &claim.Status.Conditions[i]
			break
		}
	}
	if boundCondition == nil {
		t.Fatal("Expected Bound condition to be set")
	}
	if boundCondition.Status != metav1.ConditionTrue {
		t.Error("Expected Bound condition status to be True")
	}
}

func TestReconcileDatabaseClaim_ClassNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-claim",
			Namespace:  "default",
			Finalizers: []string{claimFinalizer},
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:     "nonexistent-class",
			ReclaimPolicy: v4.ReclaimDelete,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(claim).
		WithStatusSubresource(claim).
		Build()

	ctx := context.Background()
	err := ReconcileDatabaseClaim(ctx, client, scheme, claim)
	if err != nil {
		t.Fatalf("ReconcileDatabaseClaim() failed: %v", err)
	}

	// Verify claim is pending
	if claim.Status.Phase != v4.ClaimPhasePending {
		t.Errorf("Expected phase 'Pending', got '%s'", claim.Status.Phase)
	}

	// Verify condition
	var boundCondition *metav1.Condition
	for i := range claim.Status.Conditions {
		if claim.Status.Conditions[i].Type == "Bound" {
			boundCondition = &claim.Status.Conditions[i]
			break
		}
	}
	if boundCondition == nil {
		t.Fatal("Expected Bound condition to be set")
	}
	if boundCondition.Status != metav1.ConditionFalse {
		t.Error("Expected Bound condition status to be False")
	}
	if boundCondition.Reason != "ClassNotFound" {
		t.Errorf("Expected reason 'ClassNotFound', got '%s'", boundCondition.Reason)
	}
}

func TestReconcileDatabaseClaim_ParameterOverrides(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	class := &v4.DatabaseClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cnpg-standard",
		},
		Spec: v4.DatabaseClassSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
		},
	}

	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-claim",
			Namespace:  "default",
			UID:        "claim-uid",
			Finalizers: []string{claimFinalizer},
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:     "cnpg-standard",
			ReclaimPolicy: v4.ReclaimDelete,
			Parameters: map[string]string{
				"cnpg.storageSize": "50Gi",
				"cnpg.instances":   "3",
				"minMemory":        "8Gi",
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(class, claim).
		WithStatusSubresource(claim).
		Build()

	ctx := context.Background()
	err := ReconcileDatabaseClaim(ctx, client, scheme, claim)
	if err != nil {
		t.Fatalf("ReconcileDatabaseClaim() failed: %v", err)
	}

	// Verify DatabaseCluster has overrides applied
	var cluster v4.DatabaseCluster
	err = client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-claim"}, &cluster)
	if err != nil {
		t.Fatalf("Failed to get DatabaseCluster: %v", err)
	}

	// Check CNPG overrides
	if cluster.Spec.Provider.CNPG.Storage.Size != "50Gi" {
		t.Errorf("Expected storage size '50Gi', got '%s'", cluster.Spec.Provider.CNPG.Storage.Size)
	}
	if cluster.Spec.Provider.CNPG.Instances != 3 {
		t.Errorf("Expected instances 3, got %d", cluster.Spec.Provider.CNPG.Instances)
	}

	// Check requirements override
	if cluster.Spec.Requirements == nil {
		t.Fatal("Expected requirements to be set")
	}
	if cluster.Spec.Requirements.MinMemory != "8Gi" {
		t.Errorf("Expected minMemory '8Gi', got '%s'", cluster.Spec.Requirements.MinMemory)
	}
}

func TestReconcileDatabaseClaim_HandlesDeletion_Delete(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	now := metav1.Now()
	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-claim",
			Namespace:         "default",
			UID:               "claim-uid",
			Finalizers:        []string{claimFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:     "cnpg-standard",
			ReclaimPolicy: v4.ReclaimDelete,
		},
		Status: v4.DatabaseClaimStatus{
			BoundRef: &v4.LocalRef{Name: "test-claim"},
		},
	}

	// Create the cluster that should be deleted
	cluster := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Engine: "Postgres",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(claim, cluster).
		WithStatusSubresource(claim).
		Build()

	ctx := context.Background()
	err := ReconcileDatabaseClaim(ctx, client, scheme, claim)
	if err != nil {
		t.Fatalf("ReconcileDatabaseClaim() failed during deletion: %v", err)
	}

	// Verify finalizer was removed
	hasFinalizer := false
	for _, f := range claim.Finalizers {
		if f == claimFinalizer {
			hasFinalizer = true
			break
		}
	}
	if hasFinalizer {
		t.Error("Expected finalizer to be removed")
	}

	// Verify cluster was deleted
	var deletedCluster v4.DatabaseCluster
	err = client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-claim"}, &deletedCluster)
	if err == nil {
		t.Error("Expected DatabaseCluster to be deleted, but it still exists")
	}
}

func TestReconcileDatabaseClaim_HandlesDeletion_Retain(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	now := metav1.Now()
	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-claim",
			Namespace:         "default",
			UID:               "claim-uid",
			Finalizers:        []string{claimFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:     "cnpg-standard",
			ReclaimPolicy: v4.ReclaimRetain,
		},
		Status: v4.DatabaseClaimStatus{
			BoundRef: &v4.LocalRef{Name: "test-claim"},
		},
	}

	// Create the cluster that should be retained
	cluster := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Engine: "Postgres",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(claim, cluster).
		WithStatusSubresource(claim).
		Build()

	ctx := context.Background()
	err := ReconcileDatabaseClaim(ctx, client, scheme, claim)
	if err != nil {
		t.Fatalf("ReconcileDatabaseClaim() failed during deletion with Retain: %v", err)
	}

	// Verify finalizer was removed
	hasFinalizer := false
	for _, f := range claim.Finalizers {
		if f == claimFinalizer {
			hasFinalizer = true
			break
		}
	}
	if hasFinalizer {
		t.Error("Expected finalizer to be removed")
	}

	// Verify cluster still exists
	var retainedCluster v4.DatabaseCluster
	err = client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-claim"}, &retainedCluster)
	if err != nil {
		t.Error("Expected DatabaseCluster to be retained, but it was deleted")
	}
}

func TestSynthesizeCluster(t *testing.T) {
	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim",
			Namespace: "default",
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:            "cnpg-standard",
			ConnectionSecretName: "custom-secret",
			ReclaimPolicy:        v4.ReclaimRetain,
		},
	}

	class := &v4.DatabaseClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cnpg-standard",
		},
		Spec: v4.DatabaseClassSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 3,
					Storage: v4.StorageSpec{
						Size:             "100Gi",
						StorageClassName: "gp3",
					},
				},
			},
			Requirements: &v4.DatabaseRequirements{
				MinMemory:        "16Gi",
				HighAvailability: true,
			},
		},
	}

	cluster, err := synthesizeCluster(claim, class)
	if err != nil {
		t.Fatalf("synthesizeCluster() failed: %v", err)
	}

	// Verify basic fields
	if cluster.Name != "test-claim" {
		t.Errorf("Expected name 'test-claim', got '%s'", cluster.Name)
	}
	if cluster.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", cluster.Namespace)
	}

	// Verify spec from class
	if cluster.Spec.Engine != "Postgres" {
		t.Errorf("Expected engine 'Postgres', got '%s'", cluster.Spec.Engine)
	}
	if cluster.Spec.Provider.Type != "CNPG" {
		t.Errorf("Expected provider type 'CNPG', got '%s'", cluster.Spec.Provider.Type)
	}
	if cluster.Spec.Provider.CNPG.Instances != 3 {
		t.Errorf("Expected instances 3, got %d", cluster.Spec.Provider.CNPG.Instances)
	}

	// Verify requirements copied
	if cluster.Spec.Requirements == nil {
		t.Fatal("Expected requirements to be set")
	}
	if cluster.Spec.Requirements.MinMemory != "16Gi" {
		t.Errorf("Expected minMemory '16Gi', got '%s'", cluster.Spec.Requirements.MinMemory)
	}

	// Verify connection secret from claim
	if cluster.Spec.Connection.SecretName != "custom-secret" {
		t.Errorf("Expected connection secret 'custom-secret', got '%s'", cluster.Spec.Connection.SecretName)
	}

	// Verify reclaim policy from claim
	if cluster.Spec.ReclaimPolicy != "Retain" {
		t.Errorf("Expected reclaim policy 'Retain', got '%s'", cluster.Spec.ReclaimPolicy)
	}

	// Verify labels
	if cluster.Labels["database.splunk.com/claim"] != "test-claim" {
		t.Error("Expected claim label to be set")
	}
	if cluster.Labels["database.splunk.com/class"] != "cnpg-standard" {
		t.Error("Expected class label to be set")
	}
}

func TestApplyParameterOverrides_InvalidParameter(t *testing.T) {
	cluster := &v4.DatabaseCluster{}
	class := &v4.DatabaseClass{
		Spec: v4.DatabaseClassSpec{
			Policy: &v4.ClassPolicy{
				AllowInlineOverrides: []string{"minMemory"},
			},
		},
	}

	// Try to override a parameter that's not allowed
	params := map[string]string{
		"minMemory":        "8Gi",
		"cnpg.storageSize": "50Gi", // Not allowed
	}

	err := applyParameterOverrides(cluster, class, params)
	if err == nil {
		t.Error("Expected error for disallowed parameter override, got nil")
	}
	if err != nil && err.Error() != `parameter override "cnpg.storageSize" is not allowed by class policy` {
		t.Errorf("Unexpected error message: %v", err)
	}
}
