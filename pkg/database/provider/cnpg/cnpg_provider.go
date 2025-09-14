package cnpg

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/database/provider"
)

var (
	gvkCluster     = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"}
	gvkBackup      = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Backup"}
	gvkSchedBackup = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "ScheduledBackup"}
	gvkPooler      = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Pooler"}
)

type CNPGProvider struct{ Client client.Client }

func (p *CNPGProvider) Name() string { return "CNPG" }

func (p *CNPGProvider) Ensure(ctx context.Context, db *v4.DatabaseCluster) (provider.ReconcileResult, error) {
	// Build a controller ownerRef for children (CNPG resources) owned by the DatabaseCluster
	ownerRef := metav1.OwnerReference{
		APIVersion: v4.GroupVersion.String(),
		Kind:       "DatabaseCluster",
		Name:       db.Name,
		UID:        db.UID,
		Controller: ptr(true),
	}

	// 1) Upsert CNPG Cluster
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(gvkCluster)
	cluster.SetName(db.Name)
	cluster.SetNamespace(db.Namespace)

	spec := map[string]interface{}{}
	if db.Spec.CNPG != nil {
		// unstructured JSON only supports float64/int64, not int32
		spec["instances"] = int64(db.Spec.CNPG.Instances)
		st := map[string]interface{}{"size": db.Spec.CNPG.Storage.Size}
		if sc := db.Spec.CNPG.Storage.StorageClassName; sc != "" {
			st["storageClass"] = sc
		}
		spec["storage"] = st
	}

	// Optional bootstrap
	if db.Spec.Init != nil && db.Spec.Init.CreateDatabaseIfMissing {
		spec["bootstrap"] = map[string]interface{}{
			"initdb": map[string]interface{}{
				"database": db.Spec.Init.DatabaseName,
				"owner":    db.Spec.Init.Owner,
			},
		}
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

	if err := upsert(ctx, p.Client, cluster, spec, &ownerRef); err != nil {
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
		if err := upsert(ctx, p.Client, sb, sbSpec, &ownerRef); err != nil {
			return provider.ReconcileResult{}, err
		}
	}

	// 3) Pooler if configured
	if db.Spec.CNPG != nil && db.Spec.CNPG.Pooler != nil && db.Spec.CNPG.Pooler.Enabled {
		pl := &unstructured.Unstructured{}
		pl.SetGroupVersionKind(gvkPooler)
		pl.SetName(db.Name + "-pooler-rw")
		pl.SetNamespace(db.Namespace)
		plSpec := map[string]interface{}{
			"cluster": map[string]interface{}{"name": db.Name},
			"type":    db.Spec.CNPG.Pooler.Mode, // "rw" or "ro"
		}
		if err := upsert(ctx, p.Client, pl, plSpec, &ownerRef); err != nil {
			return provider.ReconcileResult{}, err
		}
	}

	// 4) Readiness (best-effort)
	ready := cnpgReady(ctx, p.Client, db)

	return provider.ReconcileResult{
		Ready:                ready,
		ConnectionSecretName: connectionSecretName(db),
		RWEndpoint:           fmt.Sprintf("%s-rw.%s.svc.cluster.local:5432", db.Name, db.Namespace),
		ROEndpoint:           fmt.Sprintf("%s-ro.%s.svc.cluster.local:5432", db.Name, db.Namespace),
		Version:              db.Spec.Version,
	}, nil
}

func (p *CNPGProvider) EnsureBackupPlan(ctx context.Context, db *v4.DatabaseCluster) (provider.BackupStatus, error) {
	return provider.BackupStatus{}, nil
}
func (p *CNPGProvider) Restore(ctx context.Context, db *v4.DatabaseCluster, spec provider.RestoreSpec) error {
	return nil
}
func (p *CNPGProvider) ValidateConnectivity(ctx context.Context, db *v4.DatabaseCluster) error {
	return nil
}

func (p *CNPGProvider) TriggerAdhocBackup(ctx context.Context, db *v4.DatabaseBackup, src *v4.DatabaseCluster) (string, error) {
	// create a CNPG Backup: name "<dbbk>-cnpg"
	name := db.Name + "-cnpg"
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(gvkBackup)
	b.SetNamespace(db.Namespace)
	b.SetName(name)

	spec := map[string]interface{}{
		"cluster": map[string]interface{}{"name": db.Spec.TargetRef.Name},
	}
	if db.Spec.BackupLabel != "" {
		spec["backupOwnerReference"] = "cluster"
		// CNPG doesn't arbitrarily store labels in spec; OK to add meta labels instead:
		if b.GetLabels() == nil {
			b.SetLabels(map[string]string{})
		}
		b.GetLabels()["database.splunk.com/backup-label"] = db.Spec.BackupLabel
	}
	// ownerRef to DatabaseBackup
	owner := metav1.OwnerReference{
		APIVersion: v4.GroupVersion.String(),
		Kind:       "DatabaseBackup",
		Name:       db.Name,
		UID:        db.UID,
		Controller: ptr(true),
	}
	if err := upsert(ctx, p.Client, b, spec, &owner); err != nil {
		return "", err
	}

	return name, nil
}

// read status fields back from CNPG Backup into our DBBackup
func (p *CNPGProvider) InspectBackup(ctx context.Context, ns, name string) (phase string, backupID string, completed *metav1.Time, message string, err error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkBackup)
	if err = p.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, u); err != nil {
		return
	}
	phase, _, _ = unstructured.NestedString(u.Object, "status", "phase")
	phase = strings.ToLower(phase)

	backupID, _, _ = unstructured.NestedString(u.Object, "status", "backupID")
	if ts, found, _ := unstructured.NestedString(u.Object, "status", "completed"); found && ts != "" {
		t := metav1.Now()
		completed = &t
	}
	msg, _, _ := unstructured.NestedString(u.Object, "status", "error")
	message = msg
	return
}

func (p *CNPGProvider) RestoreNewClusterFromBackup(ctx context.Context, src *v4.DatabaseCluster, restore *v4.DatabaseRestore) error {
	// Create a CNPG Cluster with bootstrap.recovery.backup.name
	nc := &unstructured.Unstructured{}
	nc.SetGroupVersionKind(gvkCluster)
	nc.SetNamespace(restore.Namespace)
	newName := restore.Spec.NewCluster.Name
	if newName == "" {
		newName = restore.Name + "-restored"
	}
	nc.SetName(newName)

	instances := int64(1)
	if restore.Spec.NewCluster != nil && restore.Spec.NewCluster.Instances != nil {
		instances = int64(*restore.Spec.NewCluster.Instances)
	}
	st := map[string]interface{}{}
	if restore.Spec.NewCluster != nil {
		if restore.Spec.NewCluster.StorageSize != "" {
			st["size"] = restore.Spec.NewCluster.StorageSize
		}
		if restore.Spec.NewCluster.StorageClassName != "" {
			st["storageClass"] = restore.Spec.NewCluster.StorageClassName
		}
	}
	spec := map[string]interface{}{
		"instances": instances,
		"storage":   st,
		"bootstrap": map[string]interface{}{
			"recovery": map[string]interface{}{
				"backup": map[string]interface{}{
					"name": pickBackupName(restore),
				},
			},
		},
	}
	owner := metav1.OwnerReference{
		APIVersion: v4.GroupVersion.String(),
		Kind:       "DatabaseRestore",
		Name:       restore.Name,
		UID:        restore.UID,
		Controller: ptr(true),
	}
	return upsert(ctx, p.Client, nc, spec, &owner)
}

func (p *CNPGProvider) EnsurePooler(ctx context.Context, pooler *v4.DatabasePooler) (service string, ready bool, err error) {
	pl := &unstructured.Unstructured{}
	pl.SetGroupVersionKind(gvkPooler)
	pl.SetNamespace(pooler.Namespace)
	pl.SetName(pooler.Name)

	spec := map[string]interface{}{
		"cluster": map[string]interface{}{"name": pooler.Spec.ClusterRef.Name},
		"type":    string(pooler.Spec.Mode), // "rw"|"ro"
	}
	owner := metav1.OwnerReference{
		APIVersion: v4.GroupVersion.String(),
		Kind:       "DatabasePooler",
		Name:       pooler.Name,
		UID:        pooler.UID,
		Controller: ptr(true),
	}
	if err = upsert(ctx, p.Client, pl, spec, &owner); err != nil {
		return
	}

	// infer readiness & service name from status (best-effort)
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkPooler)
	if err = p.Client.Get(ctx, client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}, u); err != nil {
		return
	}
	svc, _, _ := unstructured.NestedString(u.Object, "status", "serviceName")
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	return svc, phase == "Ready", nil
}

func pickBackupName(r *v4.DatabaseRestore) string {
	if r.Spec.From.CNPGBackupName != "" {
		return r.Spec.From.CNPGBackupName
	}
	if r.Spec.From.DatabaseBackupRef != nil {
		// We use the convention "<dbbackup>-cnpg"
		return r.Spec.From.DatabaseBackupRef.Name + "-cnpg"
	}
	return ""
}

// ---------- helpers ----------

func upsert(ctx context.Context, c client.Client, obj *unstructured.Unstructured, spec map[string]interface{}, owner *metav1.OwnerReference) error {
	key := client.ObjectKey{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	if err := c.Get(ctx, key, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		// Create
		u := obj.DeepCopy()
		if owner != nil {
			u.SetOwnerReferences([]metav1.OwnerReference{*owner})
		}
		_ = unstructured.SetNestedField(u.Object, spec, "spec")
		return c.Create(ctx, u)
	}

	_ = unstructured.SetNestedField(existing.Object, spec, "spec")
	return c.Update(ctx, existing)
}

func cnpgReady(ctx context.Context, c client.Client, db *v4.DatabaseCluster) bool {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkCluster)
	if err := c.Get(ctx, client.ObjectKey{Namespace: db.Namespace, Name: db.Name}, u); err != nil {
		return false
	}
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	ri, _, _ := unstructured.NestedInt64(u.Object, "status", "readyInstances")
	want := int64(0)
	if db.Spec.CNPG != nil {
		want = int64(db.Spec.CNPG.Instances)
	}
	return phase == "Ready" || (want > 0 && ri >= want)
}

func connectionSecretName(db *v4.DatabaseCluster) string {
	if db.Spec.Connection != nil && db.Spec.Connection.SecretName != "" {
		return db.Spec.Connection.SecretName
	}
	return db.Name + "-conn"
}

func ptr[T any](v T) *T { return &v }
