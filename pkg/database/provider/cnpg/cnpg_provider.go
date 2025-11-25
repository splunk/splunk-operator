package cnpg

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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

type CNPGProvider struct {
	Client client.Client
}

func (p *CNPGProvider) Name() string {
	return "CNPG"
}

func (p *CNPGProvider) Ensure(ctx context.Context, db *v4.DatabaseCluster) (provider.ReconcileResult, error) {
	cnpgCfg := db.Spec.Provider.CNPG
	if cnpgCfg == nil {
		return provider.ReconcileResult{}, fmt.Errorf("CNPG provider config is nil")
	}

	// Build controller ownerRef for CNPG resources
	ownerRef := metav1.OwnerReference{
		APIVersion:         v4.GroupVersion.String(),
		Kind:               "DatabaseCluster",
		Name:               db.Name,
		UID:                db.UID,
		Controller:         ptr(true),
		BlockOwnerDeletion: ptr(true),
	}

	// 1) Upsert CNPG Cluster
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(gvkCluster)
	cluster.SetName(db.Name)
	cluster.SetNamespace(db.Namespace)

	spec := buildCNPGClusterSpec(db, cnpgCfg)

	if err := upsert(ctx, p.Client, cluster, spec, &ownerRef); err != nil {
		return provider.ReconcileResult{}, fmt.Errorf("failed to upsert CNPG cluster: %w", err)
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
			return provider.ReconcileResult{}, fmt.Errorf("failed to upsert scheduled backup: %w", err)
		}
	}

	// 3) Pooler if configured
	if cnpgCfg.Pooler != nil && cnpgCfg.Pooler.Enabled {
		pl := &unstructured.Unstructured{}
		pl.SetGroupVersionKind(gvkPooler)
		pl.SetName(db.Name + "-pooler-rw")
		pl.SetNamespace(db.Namespace)
		plSpec := map[string]interface{}{
			"cluster": map[string]interface{}{"name": db.Name},
			"type":    cnpgCfg.Pooler.Mode,
		}
		if err := upsert(ctx, p.Client, pl, plSpec, &ownerRef); err != nil {
			return provider.ReconcileResult{}, fmt.Errorf("failed to upsert pooler: %w", err)
		}
	}

	// 4) Read CNPG cluster status and connection secret
	ready := cnpgReady(ctx, p.Client, db)
	connectionData, err := p.extractConnectionData(ctx, db)
	if err != nil {
		return provider.ReconcileResult{}, fmt.Errorf("failed to extract connection data: %w", err)
	}

	return provider.ReconcileResult{
		Ready:                ready,
		ConnectionSecretData: connectionData,
		Endpoints: provider.Endpoints{
			ReadWrite: fmt.Sprintf("%s-rw.%s.svc.cluster.local", db.Name, db.Namespace),
			ReadOnly:  fmt.Sprintf("%s-ro.%s.svc.cluster.local", db.Name, db.Namespace),
		},
		Version: getClusterVersion(ctx, p.Client, db),
	}, nil
}

func (p *CNPGProvider) Delete(ctx context.Context, db *v4.DatabaseCluster) error {
	// Delete CNPG Cluster - other resources will be garbage collected via owner references
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(gvkCluster)
	cluster.SetName(db.Name)
	cluster.SetNamespace(db.Namespace)

	err := p.Client.Delete(ctx, cluster)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete CNPG cluster: %w", err)
	}
	return nil
}

func (p *CNPGProvider) GetStatus(ctx context.Context, db *v4.DatabaseCluster) (provider.Status, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkCluster)
	if err := p.Client.Get(ctx, client.ObjectKey{Namespace: db.Namespace, Name: db.Name}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return provider.Status{Phase: "Pending", Message: "CNPG cluster not found"}, nil
		}
		return provider.Status{}, err
	}

	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	readyInstances, _, _ := unstructured.NestedInt64(u.Object, "status", "readyInstances")
	instances, _, _ := unstructured.NestedInt64(u.Object, "status", "instances")

	// Map CNPG phases to our standard phases
	var standardPhase string
	var message string
	switch strings.ToLower(phase) {
	case "cluster in healthy state", "ready":
		standardPhase = "Ready"
		message = fmt.Sprintf("%d/%d instances ready", readyInstances, instances)
	case "creating resources", "upgrading cluster":
		standardPhase = "Provisioning"
		message = phase
	case "cluster is not ready":
		standardPhase = "Degraded"
		message = fmt.Sprintf("%d/%d instances ready", readyInstances, instances)
	default:
		standardPhase = "Unknown"
		message = phase
	}

	return provider.Status{
		Phase:   standardPhase,
		Message: message,
	}, nil
}

func (p *CNPGProvider) TriggerBackup(ctx context.Context, backup *v4.DatabaseBackup, src *v4.DatabaseCluster) (string, error) {
	// Create a CNPG Backup: name "<dbbk>-cnpg"
	name := backup.Name + "-cnpg"
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(gvkBackup)
	b.SetNamespace(backup.Namespace)
	b.SetName(name)

	spec := map[string]interface{}{
		"cluster": map[string]interface{}{"name": backup.Spec.TargetRef.Name},
	}
	if backup.Spec.BackupLabel != "" {
		spec["backupOwnerReference"] = "cluster"
		// Add label for tracking
		if b.GetLabels() == nil {
			b.SetLabels(map[string]string{})
		}
		labels := b.GetLabels()
		labels["database.splunk.com/backup-label"] = backup.Spec.BackupLabel
		b.SetLabels(labels)
	}

	// OwnerRef to DatabaseBackup
	owner := metav1.OwnerReference{
		APIVersion:         v4.GroupVersion.String(),
		Kind:               "DatabaseBackup",
		Name:               backup.Name,
		UID:                backup.UID,
		Controller:         ptr(true),
		BlockOwnerDeletion: ptr(true),
	}
	if err := upsert(ctx, p.Client, b, spec, &owner); err != nil {
		return "", fmt.Errorf("failed to create CNPG backup: %w", err)
	}

	return name, nil
}

func (p *CNPGProvider) GetBackupStatus(ctx context.Context, ns, backupID string) (phase string, completed *metav1.Time, message string, err error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkBackup)
	if err = p.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: backupID}, u); err != nil {
		return
	}

	phaseRaw, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	phase = strings.ToLower(phaseRaw)

	if ts, found, _ := unstructured.NestedString(u.Object, "status", "stopTime"); found && ts != "" {
		now := metav1.Now()
		completed = &now
	}

	msg, _, _ := unstructured.NestedString(u.Object, "status", "error")
	message = msg
	return
}

func (p *CNPGProvider) RestoreBackup(ctx context.Context, src *v4.DatabaseCluster, restore *v4.DatabaseRestore) error {
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

	// CRITICAL: Copy S3 backup configuration from source DatabaseCluster
	// CNPG needs this to download the backup and WAL files from S3
	if src.Spec.Backup != nil && src.Spec.Backup.Enabled && src.Spec.Provider.CNPG != nil && src.Spec.Provider.CNPG.ObjectStore != nil && src.Spec.Provider.CNPG.ObjectStore.S3 != nil {
		s3 := src.Spec.Provider.CNPG.ObjectStore.S3
		spec["backup"] = map[string]interface{}{
			"barmanObjectStore": map[string]interface{}{
				"destinationPath": fmt.Sprintf("s3://%s/%s", s3.Bucket, s3.Path),
				"s3Credentials": map[string]interface{}{
					"accessKeyId":     map[string]interface{}{"name": s3.CredentialsSecret, "key": "accessKey"},
					"secretAccessKey": map[string]interface{}{"name": s3.CredentialsSecret, "key": "secretKey"},
					"region":          map[string]interface{}{"name": s3.CredentialsSecret, "key": "region"},
				},
			},
		}
		if src.Spec.Backup.RetentionDays > 0 {
			spec["backup"].(map[string]interface{})["retentionPolicy"] = fmt.Sprintf("%dd", src.Spec.Backup.RetentionDays)
		}
	}

	owner := metav1.OwnerReference{
		APIVersion:         v4.GroupVersion.String(),
		Kind:               "DatabaseRestore",
		Name:               restore.Name,
		UID:                restore.UID,
		Controller:         ptr(true),
		BlockOwnerDeletion: ptr(true),
	}

	if err := upsert(ctx, p.Client, nc, spec, &owner); err != nil {
		return fmt.Errorf("failed to create restore cluster: %w", err)
	}
	return nil
}

func (p *CNPGProvider) EnsurePooler(ctx context.Context, pooler *v4.DatabasePooler) (service string, ready bool, err error) {
	pl := &unstructured.Unstructured{}
	pl.SetGroupVersionKind(gvkPooler)
	pl.SetNamespace(pooler.Namespace)
	pl.SetName(pooler.Name)

	spec := map[string]interface{}{
		"cluster": map[string]interface{}{"name": pooler.Spec.ClusterRef.Name},
		"type":    string(pooler.Spec.Mode),
	}

	owner := metav1.OwnerReference{
		APIVersion:         v4.GroupVersion.String(),
		Kind:               "DatabasePooler",
		Name:               pooler.Name,
		UID:                pooler.UID,
		Controller:         ptr(true),
		BlockOwnerDeletion: ptr(true),
	}

	if err = upsert(ctx, p.Client, pl, spec, &owner); err != nil {
		return
	}

	// Infer readiness & service name from status
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkPooler)
	if err = p.Client.Get(ctx, client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}, u); err != nil {
		return
	}

	svc, _, _ := unstructured.NestedString(u.Object, "status", "serviceName")
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	return svc, phase == "Ready", nil
}

// ---------- Helper Functions ----------

func buildCNPGClusterSpec(db *v4.DatabaseCluster, cnpgCfg *v4.CNPGProviderSpec) map[string]interface{} {
	spec := map[string]interface{}{
		"instances": int64(cnpgCfg.Instances),
		"storage": map[string]interface{}{
			"size": cnpgCfg.Storage.Size,
		},
	}

	if cnpgCfg.Storage.StorageClassName != "" {
		spec["storage"].(map[string]interface{})["storageClass"] = cnpgCfg.Storage.StorageClassName
	}

	// Resources
	if cnpgCfg.Resources != nil {
		spec["resources"] = cnpgCfg.Resources
	}

	// Bootstrap initdb
	if cnpgCfg.BootstrapInitDB != nil && cnpgCfg.BootstrapInitDB.CreateDatabaseIfMissing {
		spec["bootstrap"] = map[string]interface{}{
			"initdb": map[string]interface{}{
				"database": cnpgCfg.BootstrapInitDB.DatabaseName,
				"owner":    cnpgCfg.BootstrapInitDB.Owner,
			},
		}
	}

	// Backup configuration
	if db.Spec.Backup != nil && db.Spec.Backup.Enabled && cnpgCfg.ObjectStore != nil && cnpgCfg.ObjectStore.S3 != nil {
		s3 := cnpgCfg.ObjectStore.S3
		spec["backup"] = map[string]interface{}{
			"barmanObjectStore": map[string]interface{}{
				"destinationPath": fmt.Sprintf("s3://%s/%s", s3.Bucket, s3.Path),
				"s3Credentials": map[string]interface{}{
					"accessKeyId":     map[string]interface{}{"name": s3.CredentialsSecret, "key": "accessKey"},
					"secretAccessKey": map[string]interface{}{"name": s3.CredentialsSecret, "key": "secretKey"},
					"region":          map[string]interface{}{"name": s3.CredentialsSecret, "key": "region"},
				},
			},
		}
		if db.Spec.Backup.RetentionDays > 0 {
			spec["backup"].(map[string]interface{})["retentionPolicy"] = fmt.Sprintf("%dd", db.Spec.Backup.RetentionDays)
		}
	}

	// TLS/Certificate configuration
	if cnpgCfg.ServerCert != nil || cnpgCfg.ClientCert != nil {
		certificates := map[string]interface{}{}

		// Server certificate
		if cnpgCfg.ServerCert != nil {
			serverCert := map[string]interface{}{}

			// If using cert-manager
			if cnpgCfg.ServerCert.IssuerRef != nil {
				serverCert["serverTLSSecret"] = db.Name + "-server-cert"
				serverCert["serverCASecret"] = db.Name + "-server-ca"
			} else if cnpgCfg.ServerCert.SecretName != "" {
				// Using existing secret
				serverCert["serverTLSSecret"] = cnpgCfg.ServerCert.SecretName
				if cnpgCfg.ServerCert.CASecretName != "" {
					serverCert["serverCASecret"] = cnpgCfg.ServerCert.CASecretName
				}
			}

			if len(serverCert) > 0 {
				for k, v := range serverCert {
					certificates[k] = v
				}
			}
		}

		// Client certificate (for replication)
		if cnpgCfg.ClientCert != nil {
			clientCert := map[string]interface{}{}

			if cnpgCfg.ClientCert.IssuerRef != nil {
				clientCert["replicationTLSSecret"] = db.Name + "-replication-cert"
				clientCert["clientCASecret"] = db.Name + "-client-ca"
			} else if cnpgCfg.ClientCert.SecretName != "" {
				clientCert["replicationTLSSecret"] = cnpgCfg.ClientCert.SecretName
				if cnpgCfg.ClientCert.CASecretName != "" {
					clientCert["clientCASecret"] = cnpgCfg.ClientCert.CASecretName
				}
			}

			if len(clientCert) > 0 {
				for k, v := range clientCert {
					certificates[k] = v
				}
			}
		}

		if len(certificates) > 0 {
			spec["certificates"] = certificates
		}
	}

	// ImageName for PostgreSQL version control
	if cnpgCfg.ImageName != "" {
		spec["imageName"] = cnpgCfg.ImageName
	}

	// ImageCatalogRef for PostgreSQL version management via CNPG ClusterImageCatalog
	if cnpgCfg.ImageCatalogRef != nil {
		imageCatalogRef := map[string]interface{}{
			"name": cnpgCfg.ImageCatalogRef.Name,
		}
		if cnpgCfg.ImageCatalogRef.Kind != "" {
			imageCatalogRef["kind"] = cnpgCfg.ImageCatalogRef.Kind
		}
		if cnpgCfg.ImageCatalogRef.APIGroup != "" {
			imageCatalogRef["apiGroup"] = cnpgCfg.ImageCatalogRef.APIGroup
		}
		spec["imageCatalogRef"] = imageCatalogRef
	}

	// ServiceAccountTemplate for IRSA and other ServiceAccount customizations
	if cnpgCfg.ServiceAccountTemplate != nil {
		saTemplate := map[string]interface{}{}

		// Build metadata section
		metadata := map[string]interface{}{}
		if len(cnpgCfg.ServiceAccountTemplate.Metadata.Annotations) > 0 {
			metadata["annotations"] = cnpgCfg.ServiceAccountTemplate.Metadata.Annotations
		}
		if len(cnpgCfg.ServiceAccountTemplate.Metadata.Labels) > 0 {
			metadata["labels"] = cnpgCfg.ServiceAccountTemplate.Metadata.Labels
		}

		if len(metadata) > 0 {
			saTemplate["metadata"] = metadata
		}

		if len(saTemplate) > 0 {
			spec["serviceAccountTemplate"] = saTemplate
		}
	}

	return spec
}

func (p *CNPGProvider) extractConnectionData(ctx context.Context, db *v4.DatabaseCluster) (map[string][]byte, error) {
	// Read CNPG's generated app secret: <cluster-name>-app
	secretName := db.Name + "-app"
	secret := &corev1.Secret{}
	err := p.Client.Get(ctx, types.NamespacedName{Namespace: db.Namespace, Name: secretName}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Secret not created yet - cluster is still initializing
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get CNPG app secret: %w", err)
	}

	// Extract connection details
	host := fmt.Sprintf("%s-rw.%s.svc.cluster.local", db.Name, db.Namespace)
	port := "5432"
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	dbname := string(secret.Data["dbname"])

	if username == "" || password == "" || dbname == "" {
		return nil, fmt.Errorf("incomplete connection data in CNPG secret")
	}

	return map[string][]byte{
		"host":     []byte(host),
		"port":     []byte(port),
		"username": []byte(username),
		"password": []byte(password),
		"dbname":   []byte(dbname),
		"sslmode":  []byte("require"),
	}, nil
}

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
		// Directly assign spec instead of using SetNestedField to avoid deep copy issues
		u.Object["spec"] = spec
		return c.Create(ctx, u)
	}

	// Update
	// Directly assign spec instead of using SetNestedField to avoid deep copy issues
	existing.Object["spec"] = spec
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
	if db.Spec.Provider.CNPG != nil {
		want = int64(db.Spec.Provider.CNPG.Instances)
	}

	return strings.Contains(strings.ToLower(phase), "healthy") || strings.Contains(strings.ToLower(phase), "ready") || (want > 0 && ri >= want)
}

func getClusterVersion(ctx context.Context, c client.Client, db *v4.DatabaseCluster) string {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkCluster)
	if err := c.Get(ctx, client.ObjectKey{Namespace: db.Namespace, Name: db.Name}, u); err != nil {
		return ""
	}

	version, _, _ := unstructured.NestedString(u.Object, "status", "currentPrimaryInstanceVersion")
	return version
}

func pickBackupName(r *v4.DatabaseRestore) string {
	if r.Spec.From.CNPGBackupName != "" {
		return r.Spec.From.CNPGBackupName
	}
	if r.Spec.From.DatabaseBackupRef != nil {
		return r.Spec.From.DatabaseBackupRef.Name + "-cnpg"
	}
	return ""
}

func ptr[T any](v T) *T {
	return &v
}

// TriggerAdhocBackup creates an ad-hoc backup
func (p *CNPGProvider) TriggerAdhocBackup(ctx context.Context, backup interface{}, target interface{}) (string, error) {
	bk, ok := backup.(*v4.DatabaseBackup)
	if !ok {
		return "", fmt.Errorf("backup is not *v4.DatabaseBackup")
	}
	tgt, ok := target.(*v4.DatabaseCluster)
	if !ok {
		return "", fmt.Errorf("target is not *v4.DatabaseCluster")
	}
	return p.TriggerBackup(ctx, bk, tgt)
}

// InspectBackup inspects the status of a backup
func (p *CNPGProvider) InspectBackup(ctx context.Context, namespace, name string) (phase, backupID string, completedAt *metav1.Time, message string, err error) {
	phase, completedAt, message, err = p.GetBackupStatus(ctx, namespace, name)
	backupID = name // Use the backup name as the ID
	return
}

// RestoreNewClusterFromBackup restores a cluster from backup
func (p *CNPGProvider) RestoreNewClusterFromBackup(ctx context.Context, restore interface{}, sourceCluster interface{}) error {
	rst, ok := restore.(*v4.DatabaseRestore)
	if !ok {
		return fmt.Errorf("restore is not *v4.DatabaseRestore")
	}
	src, ok := sourceCluster.(*v4.DatabaseCluster)
	if !ok {
		return fmt.Errorf("sourceCluster is not *v4.DatabaseCluster")
	}
	return p.RestoreBackup(ctx, src, rst)
}
