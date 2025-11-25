package reconcile

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/database/conditions"
	"github.com/splunk/splunk-operator/pkg/database/provider"
	"github.com/splunk/splunk-operator/pkg/database/provider/cnpg"
)

const connectionSecretFinalizer = "database.splunk.com/connection-secret"

func providerFor(c client.Client, db *v4.DatabaseCluster) provider.Provider {
	providerType := db.Spec.Provider.Type
	switch providerType {
	case "CNPG", "":
		return &cnpg.CNPGProvider{Client: c}
	// Future: Add Aurora, CloudSQL, etc.
	default:
		// Default to CNPG for now
		return &cnpg.CNPGProvider{Client: c}
	}
}

func ReconcileDatabaseCluster(ctx context.Context, c client.Client, db *v4.DatabaseCluster) (requeueAfter time.Duration, err error) {
	l := log.FromContext(ctx)

	// Handle deletion
	if !db.DeletionTimestamp.IsZero() {
		return handleDeletion(ctx, c, db)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(db, connectionSecretFinalizer) {
		controllerutil.AddFinalizer(db, connectionSecretFinalizer)
		if err := c.Update(ctx, db); err != nil {
			return 0, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return time.Second, nil
	}

	// Get provider
	prov := providerFor(c, db)

	// Ensure database resources via provider
	res, err := prov.Ensure(ctx, db)
	if err != nil {
		l.Error(err, "provider ensure failed", "provider", prov.Name())
		updateCondition(db, "Ready", metav1.ConditionFalse, "ProviderError", err.Error())
		db.Status.Phase = "Error"
		return time.Minute, err
	}

	// Create/update normalized connection secret
	secretReady := false
	if res.ConnectionSecretData != nil && len(res.ConnectionSecretData) > 0 {
		secretName := connectionSecretName(db)
		if err := ensureConnectionSecret(ctx, c, db, secretName, res.ConnectionSecretData); err != nil {
			l.Error(err, "failed to ensure connection secret", "secret", secretName)
			updateCondition(db, "SecretsReady", metav1.ConditionFalse, "SecretCreationFailed", err.Error())
		} else {
			secretReady = true
			updateCondition(db, "SecretsReady", metav1.ConditionTrue, "SecretReady", "Connection secret created/updated")
		}
	} else if res.Ready {
		// Provider says ready but no connection data - wait
		updateCondition(db, "SecretsReady", metav1.ConditionFalse, "WaitingForCredentials", "Provider ready but connection data not available yet")
	}

	// Check certificate readiness
	certsReady := true
	if db.Spec.Connection != nil && db.Spec.Connection.SSL != nil && db.Spec.Connection.SSL.CABundleSecret != "" {
		var ca corev1.Secret
		if getErr := c.Get(ctx, client.ObjectKey{Namespace: db.Namespace, Name: db.Spec.Connection.SSL.CABundleSecret}, &ca); getErr != nil {
			certsReady = false
			updateCondition(db, "CertificatesReady", metav1.ConditionFalse, "CABundleMissing", "CA bundle secret not found")
		} else {
			updateCondition(db, "CertificatesReady", metav1.ConditionTrue, "CertificatesVerified", "TLS references resolved")
		}
	} else {
		updateCondition(db, "CertificatesReady", metav1.ConditionTrue, "NotRequired", "TLS not configured")
	}

	// Update status
	if db.Status.Endpoints == nil {
		db.Status.Endpoints = &v4.DatabaseEndpoints{}
	}
	db.Status.Endpoints.ReadWrite = res.Endpoints.ReadWrite
	db.Status.Endpoints.ReadOnly = res.Endpoints.ReadOnly
	db.Status.Endpoints.External = res.Endpoints.External
	db.Status.Provider = prov.Name()
	db.Status.Version = res.Version
	db.Status.ConnectionSecret = connectionSecretName(db)

	// Overall readiness
	ready := res.Ready && secretReady && certsReady
	if ready {
		db.Status.Phase = "Ready"
		updateCondition(db, "Ready", metav1.ConditionTrue, "AllChecksPass", "Database is ready to accept connections")
	} else {
		db.Status.Phase = "Provisioning"
		msg := buildWaitMessage(res.Ready, secretReady, certsReady)
		updateCondition(db, "Ready", metav1.ConditionFalse, "WaitingOnDependencies", msg)
	}

	// Requeue based on provider recommendation or default
	if res.RequeueAfter > 0 {
		return res.RequeueAfter, nil
	}
	return 2 * time.Minute, nil
}

func handleDeletion(ctx context.Context, c client.Client, db *v4.DatabaseCluster) (time.Duration, error) {
	l := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(db, connectionSecretFinalizer) {
		// Call provider Delete if ReclaimPolicy is Delete
		if db.Spec.ReclaimPolicy == "Delete" || db.Spec.ReclaimPolicy == "" {
			prov := providerFor(c, db)
			if err := prov.Delete(ctx, db); err != nil && !apierrors.IsNotFound(err) {
				l.Error(err, "failed to delete provider resources")
				return time.Minute, err
			}
		}

		// Delete connection secret
		secretName := connectionSecretName(db)
		secret := &corev1.Secret{}
		secret.Name = secretName
		secret.Namespace = db.Namespace
		if err := c.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			l.Error(err, "failed to delete connection secret", "secret", secretName)
			return time.Minute, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(db, connectionSecretFinalizer)
		if err := c.Update(ctx, db); err != nil {
			return 0, err
		}
	}

	return 0, nil
}

func ensureConnectionSecret(ctx context.Context, c client.Client, db *v4.DatabaseCluster, secretName string, data map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: db.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, c, secret, func() error {
		// Set owner reference
		if err := controllerutil.SetControllerReference(db, secret, c.Scheme()); err != nil {
			return fmt.Errorf("failed to set owner reference: %w", err)
		}

		// Update labels
		if secret.Labels == nil {
			secret.Labels = make(map[string]string)
		}
		secret.Labels["app.kubernetes.io/managed-by"] = "splunk-operator"
		secret.Labels["database.splunk.com/cluster"] = db.Name

		// Update secret data
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = data

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create/update connection secret: %w", err)
	}

	return nil
}

func connectionSecretName(db *v4.DatabaseCluster) string {
	if db.Spec.Connection != nil && db.Spec.Connection.SecretName != "" {
		return db.Spec.Connection.SecretName
	}
	return db.Name + "-conn"
}

func updateCondition(db *v4.DatabaseCluster, condType string, status metav1.ConditionStatus, reason, message string) {
	conditions.Upsert(&db.Status.Conditions, metav1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func buildWaitMessage(provReady, secretReady, certsReady bool) string {
	var waiting []string
	if !provReady {
		waiting = append(waiting, "provider")
	}
	if !secretReady {
		waiting = append(waiting, "connection secret")
	}
	if !certsReady {
		waiting = append(waiting, "certificates")
	}

	if len(waiting) == 0 {
		return "All checks passed"
	}

	return fmt.Sprintf("Waiting on: %v", waiting)
}
