package cnpg

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dbv1 "github.com/splunk/splunk-operator/api/database/v1alpha1"
	"github.com/splunk/splunk-operator/internal/controller/database/provider"
)

var (
	gvkCluster     = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"}
	gvkSchedBackup = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "ScheduledBackup"}
	gvkPooler      = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Pooler"}
)

type CNPGProvider struct{ Client client.Client }

func (p *CNPGProvider) Name() string { return "CNPG" }

func (p *CNPGProvider) Ensure(ctx context.Context, db *dbv1.DatabaseCluster) (provider.ReconcileResult, error) {
	// 1) Upsert CNPG Cluster
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(gvkCluster)
	cluster.SetName(db.Name)
	cluster.SetNamespace(db.Namespace)

	spec := map[string]interface{}{
		"instances": db.Spec.CNPG.Instances,
		"storage": map[string]interface{}{
			"size": db.Spec.CNPG.Storage.Size,
		},
	}
	if sc := db.Spec.CNPG.Storage.StorageClassName; sc != "" {
		spec["storage"].(map[string]interface{})["storageClass"] = sc
	}

	// Optional bootstrap
	if db.Spec.Init != nil && db.Spec.Init.CreateDatabaseIfMissing {
		b := map[string]interface{}{
			"initdb": map[string]interface{}{
				"database": db.Spec.Init.DatabaseName,
				"owner":    db.Spec.Init.Owner,
			},
		}
		spec["bootstrap"] = b
	}

	// Optional backups via barmanObjectStore
	if db.Spec.Backup != nil && db.Spec.Backup.Enabled && db.Spec.Backup.ObjectStore != nil && db.Spec.Backup.ObjectStore.S3 != nil {
		s3 := db.Spec.Backup.ObjectStore.S3
		spec["backup"] = map[string]interface{}{
			"barmanObjectStore": map[string]interface{}{
				"destinationPath": fmt.Sprintf("s3://%s/%s", s3.Bucket, s3.Path),
				"s3Credentials": map[string]interface{}{
					"accessKeyId":     map[string]interface{}{"name": s3.CredentialsSecret, "key": "accessKey"},
					"secretAccessKey": map[string]interface{}{"name": s3.CredentialsSecret, "key": "secretKey"},
					"region":          s3.Region,
				},
			},
		}
	}

	if err := p.upsert(ctx, cluster, spec, db); err != nil {
		return provider.ReconcileResult{}, err
	}

	// 2) ScheduledBackup if requested
	if db.Spec.Backup != nil && db.Spec.Backup.Enabled && db.Spec.Backup.Schedule != "" {
		sb := &unstructured.Unstructured{}
		sb.SetGroupVersionKind(gvkSchedBackup)
		sb.SetName(db.Name + "-scheduled")
		sb.SetNamespace(db.Namespace)
		sbSpec := map[string]interface{}{
			"schedule":             db.Spec.Backup.Schedule,
			"backupOwnerReference": "cluster",
			"cluster":              map[string]interface{}{"name": db.Name},
		}
		if err := p.upsert(ctx, sb, sbSpec, db); err != nil {
			return provider.ReconcileResult{}, err
		}
	}

	// 3) Pooler if configured
	if db.Spec.CNPG.Pooler != nil && db.Spec.CNPG.Pooler.Enabled {
		pl := &unstructured.Unstructured{}
		pl.SetGroupVersionKind(gvkPooler)
		pl.SetName(db.Name + "-pooler-rw")
		pl.SetNamespace(db.Namespace)
		plSpec := map[string]interface{}{
			"cluster": map[string]interface{}{"name": db.Name},
			"type":    db.Spec.CNPG.Pooler.Mode, // "rw" or "ro"
		}
		if err := p.upsert(ctx, pl, plSpec, db); err != nil {
			return provider.ReconcileResult{}, err
		}
	}

	// 4) Read status to detect readiness (best-effort)
	ready := false
	if st, err := p.getClusterStatus(ctx, db); err == nil {
		ready = st
	}

	return provider.ReconcileResult{
		Ready:               ready,
		ConnectionSecretName: safeSecretName(db),
		RWEndpoint:          fmt.Sprintf("%s-rw.%s.svc.cluster.local:5432", db.Name, db.Namespace),
		ROEndpoint:          fmt.Sprintf("%s-ro.%s.svc.cluster.local:5432", db.Name, db.Namespace),
		Version:             db.Spec.Version,
	}, nil
}

func (p *CNPGProvider) Delete(ctx context.Context, db *dbv1.DatabaseCluster) error { return nil }
func (p *CNPGProvider) EnsureBackupPlan(ctx context.Context, db *dbv1.DatabaseCluster) (provider.BackupStatus, error) {
	return provider.BackupStatus{}, nil
}
func (p *CNPGProvider) TriggerAdhocBackup(ctx context.Context, db *dbv1.DatabaseCluster) error { return nil }
func (p *CNPGProvider) Restore(ctx context.Context, db *dbv1.DatabaseCluster, spec provider.RestoreSpec) error {
	return nil
}
func (p *CNPGProvider) ValidateConnectivity(ctx context.Context, db *dbv1.DatabaseCluster) error { return nil }

func (p *CNPGProvider) upsert(ctx context.Context, obj *unstructured.Unstructured, spec map[string]interface{}, owner metav1.Object) error {
	key := client.ObjectKeyFromObject(obj)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	if err := p.Client.Get(ctx, key, existing); err != nil {
		// Create
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(obj.GroupVersionKind())
		u.SetName(obj.GetName())
		u.SetNamespace(obj.GetNamespace())
		if owner != nil {
			u.SetOwnerReferences([]metav1.OwnerReference{{
				APIVersion: owner.GetObjectKind().GroupVersionKind().GroupVersion().String(),
				Kind:       owner.GetObjectKind().GroupVersionKind().Kind,
				Name:       owner.GetName(),
				UID:        owner.GetUID(),
				Controller: ptr(true),
			}})
		}
		_ = unstructured.SetNestedField(u.Object, spec, "spec")
		return p.Client.Create(ctx, u)
	}

	_ = unstructured.SetNestedField(existing.Object, spec, "spec")
	return p.Client.Update(ctx, existing)
}

func (p *CNPGProvider) getClusterStatus(ctx context.Context, db *dbv1.DatabaseCluster) (bool, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkCluster)
	key := client.ObjectKey{Namespace: db.Namespace, Name: db.Name}
	if err := p.Client.Get(ctx, key, u); err != nil {
		return false, err
	}
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	readyInst, _, _ := unstructured.NestedInt64(u.Object, "status", "readyInstances")
	instances := int64(0)
	if db.Spec.CNPG != nil {
		instances = int64(db.Spec.CNPG.Instances)
	}
	return phase == "Ready" || (readyInst > 0 && instances > 0 && readyInst >= instances), nil
}

func safeSecretName(db *dbv1.DatabaseCluster) string {
	if db.Spec.Connection != nil && db.Spec.Connection.SecretName != "" {
		return db.Spec.Connection.SecretName
	}
	return db.Name + "-conn"
}

func ptr[T any](v T) *T { return &v }
