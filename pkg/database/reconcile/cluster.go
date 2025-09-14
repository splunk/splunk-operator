package reconcile

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/database/conditions"
	"github.com/splunk/splunk-operator/pkg/database/provider"
	"github.com/splunk/splunk-operator/pkg/database/provider/cnpg"
)

func providerFor(c client.Client, db *v4.DatabaseCluster) provider.Provider {
	switch db.Spec.ManagedBy {
	case "CNPG", "":
		return &cnpg.CNPGProvider{Client: c}
	default:
		return &cnpg.CNPGProvider{Client: c}
	}
}

func ReconcileDatabaseCluster(ctx context.Context, c client.Client, db *v4.DatabaseCluster) (requeueAfter time.Duration, err error) {
	l := log.FromContext(ctx)

	res, err := providerFor(c, db).Ensure(ctx, db)
	if err != nil {
		l.Error(err, "provider ensure failed")
		return time.Minute, err
	}

	// SecretsReady
	secretsReady := false
	if db.Spec.Connection != nil && db.Spec.Connection.SecretName != "" {
		var s corev1.Secret
		if getErr := c.Get(ctx, client.ObjectKey{Namespace: db.Namespace, Name: db.Spec.Connection.SecretName}, &s); getErr == nil {
			secretsReady = true
		}
	}
	conditions.Upsert(&db.Status.Conditions, metav1.Condition{
		Type:    "SecretsReady",
		Status:  boolToStatus(secretsReady),
		Reason:  tern(secretsReady, "Found", "Missing"),
		Message: tern(secretsReady, "Connection secret present", "Connection secret not found"),
	})

	// CertificatesReady
	certsReady := true
	if db.Spec.Connection != nil && db.Spec.Connection.SSL != nil && db.Spec.Connection.SSL.CABundleSecret != "" {
		var ca corev1.Secret
		if getErr := c.Get(ctx, client.ObjectKey{Namespace: db.Namespace, Name: db.Spec.Connection.SSL.CABundleSecret}, &ca); getErr != nil {
			certsReady = false
		}
	}
	conditions.Upsert(&db.Status.Conditions, metav1.Condition{
		Type:    "CertificatesReady",
		Status:  boolToStatus(certsReady),
		Reason:  tern(certsReady, "Verified", "Missing"),
		Message: tern(certsReady, "TLS references resolved", "CA bundle secret not found"),
	})

	// Status surface
	if db.Status.Endpoints == nil {
		db.Status.Endpoints = &v4.DatabaseEndpoints{}
	}
	db.Status.Endpoints.ReadWrite = res.RWEndpoint
	db.Status.Endpoints.ReadOnly = res.ROEndpoint
	db.Status.Provider = db.Spec.ManagedBy
	db.Status.Version = res.Version
	if db.Spec.Connection != nil {
		db.Status.ConnectionSecret = db.Spec.Connection.SecretName
	}

	ready := res.Ready && secretsReady && certsReady
	db.Status.Phase = tern(ready, "Ready", "Creating")
	conditions.Upsert(&db.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  boolToStatus(ready),
		Reason:  tern(ready, "AllChecksPass", "WaitingOnDeps"),
		Message: tern(ready, "Database ready", "Waiting on provider/secret/certificates"),
	})

	return 2 * time.Minute, nil
}

func boolToStatus(b bool) metav1.ConditionStatus {
	if b {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}
func tern[T any](b bool, x, y T) T {
	if b {
		return x
	}
	return y
}
