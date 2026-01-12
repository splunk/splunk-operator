/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databases/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases/status,verbs=get
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconciliation loop for Database resources.
// See poc/CONTROLLER-GUIDE.md for detailed implementation guide.
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Database", "name", req.Name, "namespace", req.Namespace)

	// Step 1: Fetch the Database CR
	db := &enterprisev4.Database{}
	if err := r.Get(ctx, req.NamespacedName, db); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Database resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Database")
		return ctrl.Result{}, err
	}

	// Step 2: Fetch the DatabaseClass
	dbClass := &enterprisev4.DatabaseClass{}
	if err := r.Get(ctx, client.ObjectKey{Name: db.Spec.Class}, dbClass); err != nil {
		logger.Error(err, "Failed to get DatabaseClass", "class", db.Spec.Class)
		return ctrl.Result{}, err
	}

	// Step 3: Merge configuration
	merged := r.mergeConfig(dbClass, db)

	// Step 4: Check if CNPG Cluster exists
	cnpgCluster := &cnpgv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      db.Name,
		Namespace: db.Namespace,
	}, cnpgCluster)

	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating CNPG Cluster", "name", db.Name)
		cnpgCluster, err = r.buildCNPGCluster(db, merged)
		if err != nil {
			logger.Error(err, "Failed to build CNPG Cluster")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, cnpgCluster); err != nil {
			logger.Error(err, "Failed to create CNPG Cluster")
			return ctrl.Result{}, err
		}

		// Update Database status with provisioner reference
		db.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: "postgresql.cnpg.io/v1",
			Kind:       "Cluster",
			Name:       cnpgCluster.Name,
			Namespace:  cnpgCluster.Namespace,
			UID:        cnpgCluster.UID,
		}
		db.Status.Phase = "Provisioning"

		if err := r.Status().Update(ctx, db); err != nil {
			logger.Error(err, "Failed to update Database status")
			return ctrl.Result{}, err
		}

		logger.Info("CNPG Cluster created successfully", "name", db.Name)
		// Requeue immediately to continue with database reconciliation
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get CNPG Cluster")
		return ctrl.Result{}, err
	}

	// Step 5: CNPG Cluster exists - reconcile CNPG Database CRs
	logger.Info("CNPG Cluster exists, reconciling databases", "phase", cnpgCluster.Status.Phase)

	// Build desired CNPG Database resources from config
	cnpgDatabases, err := r.buildCNPGDatabases(db, cnpgCluster, merged)
	if err != nil {
		logger.Error(err, "Failed to build CNPG Databases")
		return ctrl.Result{}, err
	}

	// Reconcile each CNPG Database CR
	for i := range cnpgDatabases {
		cnpgDB := &cnpgDatabases[i]
		existingDB := &cnpgv1.Database{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      cnpgDB.Name,
			Namespace: cnpgDB.Namespace,
		}, existingDB)

		if err != nil && errors.IsNotFound(err) {
			// Database doesn't exist, create it
			logger.Info("Creating CNPG Database", "database", cnpgDB.Name)
			if err := r.Create(ctx, cnpgDB); err != nil {
				logger.Error(err, "Failed to create CNPG Database", "database", cnpgDB.Name)
				return ctrl.Result{}, err
			}
		} else if err != nil {
			logger.Error(err, "Failed to get CNPG Database", "database", cnpgDB.Name)
			return ctrl.Result{}, err
		} else {
			// Database exists - CNPG will reconcile it if spec changed
			logger.Info("CNPG Database already exists", "database", cnpgDB.Name)
		}
	}

	// Step 6: Update Database status based on CNPG Cluster status
	logger.Info("Updating Database status", "cnpgPhase", cnpgCluster.Status.Phase)
	if err := r.updateDatabaseStatus(ctx, db, cnpgCluster); err != nil {
		logger.Error(err, "Failed to update Database status")
		return ctrl.Result{}, err
	}

	// Step 7: If CNPG is ready, generate ConfigMap and Secret with connection info
	if cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy {
		logger.Info("CNPG Cluster is ready, generating connection resources")

		// Reconcile ConfigMap
		if err := r.reconcileConfigMap(ctx, db, cnpgCluster, merged); err != nil {
			logger.Error(err, "Failed to reconcile ConfigMap")
			return ctrl.Result{}, err
		}

		// Reconcile Secret
		if err := r.reconcileSecret(ctx, db, cnpgCluster, merged); err != nil {
			logger.Error(err, "Failed to reconcile Secret")
			return ctrl.Result{}, err
		}

		logger.Info("Connection resources created successfully")
	}

	return ctrl.Result{}, nil
}

func (r *DatabaseReconciler) mergeConfig(class *enterprisev4.DatabaseClass, db *enterprisev4.Database) *enterprisev4.DatabaseConfig {
	databases := class.Spec.Config.Databases
	extensions := class.Spec.Config.Extensions
	instances := class.Spec.Config.Instances
	engineConfig := class.Spec.Config.PostgreSQL
	postgresVersion := class.Spec.Config.PostgresVersion
	storage := class.Spec.Config.Storage
	resources := class.Spec.Config.Resources

	if db.Spec.Databases != nil {
		databases = db.Spec.Databases
	}

	if db.Spec.Extensions != nil {
		extensions = db.Spec.Extensions
	}

	if db.Spec.Instances != nil {
		instances = db.Spec.Instances
	}

	if db.Spec.PostgreSQL != nil {
		engineConfig = db.Spec.PostgreSQL
	}

	if db.Spec.PostgresVersion != nil {
		postgresVersion = db.Spec.PostgresVersion
	}

	if db.Spec.Resources != nil {
		resources = db.Spec.Resources
	}

	if db.Spec.Storage != nil {
		storage = db.Spec.Storage
	}

	return &enterprisev4.DatabaseConfig{Instances: instances,
		Storage:         storage,
		PostgresVersion: postgresVersion,
		Resources:       resources,
		PostgreSQL:      engineConfig,
		Extensions:      extensions,
		Databases:       databases}
}

func (r *DatabaseReconciler) buildCNPGCluster(db *enterprisev4.Database, config *enterprisev4.DatabaseConfig) (*cnpgv1.Cluster, error) {
	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      db.Name,
			Namespace: db.Namespace,
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName:             fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *config.PostgresVersion),
			Instances:             int(*config.Instances),
			PostgresConfiguration: cnpgv1.PostgresConfiguration{Parameters: config.PostgreSQL},
			Resources:             *config.Resources,
			StorageConfiguration:  cnpgv1.StorageConfiguration{Size: config.Storage.String()},
		},
	}

	// Set owner reference so CNPG Cluster is deleted when Database is deleted
	if err := ctrl.SetControllerReference(db, cnpgCluster, r.Scheme); err != nil {
		return nil, err
	}

	return cnpgCluster, nil
}

func (r *DatabaseReconciler) buildCNPGDatabases(db *enterprisev4.Database, cnpgCluster *cnpgv1.Cluster, config *enterprisev4.DatabaseConfig) ([]cnpgv1.Database, error) {
	databases := []cnpgv1.Database{}
	extensions := []cnpgv1.ExtensionSpec{}
	for _, extension := range config.Extensions {
		extensions = append(extensions, cnpgv1.ExtensionSpec{
			DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{
				Name:   extension,
				Ensure: "present",
			},
		})
	}

	for _, database := range config.Databases {
		// Sanitize database name for Kubernetes (replace underscores with hyphens)
		sanitizedName := strings.ReplaceAll(database, "_", "-")

		cnpgDB := cnpgv1.Database{Spec: cnpgv1.DatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: cnpgCluster.Name}, Owner: "app",
			Ensure:     "present",
			Name:       database, // PostgreSQL database name (can have underscores)
			Extensions: extensions},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", cnpgCluster.Name, sanitizedName), // Kubernetes resource name (no underscores)
				Namespace: cnpgCluster.Namespace},
		}
		if err := ctrl.SetControllerReference(db, &cnpgDB, r.Scheme); err != nil {
			return nil, err
		}
		databases = append(databases, cnpgDB)
	}
	return databases, nil
}

func (r *DatabaseReconciler) updateDatabaseStatus(ctx context.Context, db *enterprisev4.Database, cnpgCluster *cnpgv1.Cluster) error {

	switch cnpgCluster.Status.Phase {
	case cnpgv1.PhaseHealthy:
		db.Status.Phase = "Ready"
		meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "ClusterHealthy",
			Message:            "CNPG cluster is in healthy state",
			ObservedGeneration: db.Generation})
	case cnpgv1.PhaseUnrecoverable:
		db.Status.Phase = "Failed"
		meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ClusterCreationFailed",
			Message:            "PostgreSQL cluster is in unrecoverable phase!",
			ObservedGeneration: db.Generation})
	case "":
		db.Status.Phase = "Pending"
		meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ClusterPending",
			Message:            "Waiting for CNPG cluster to start",
			ObservedGeneration: db.Generation})
	default:
		db.Status.Phase = "Provisioning"
		meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ClusterProvisioning",
			Message:            "Waiting for CNPG cluster to provision",
			ObservedGeneration: db.Generation})
	}

	if err := r.Status().Update(ctx, db); err != nil {
		return err
	}

	return nil
}

func (r *DatabaseReconciler) generateConfigMap(db *enterprisev4.Database, cnpgCluster *cnpgv1.Cluster, config *enterprisev4.DatabaseConfig) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-connection", db.Name),
			Namespace: db.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "database-controller",
			},
		},
		Data: map[string]string{
			"DB_SERVICE_RW": cnpgCluster.Status.WriteService,
			"DB_SERVICE_RO": cnpgCluster.Status.ReadService,
			"DB_PORT":       "5432",
			"DB_NAMES":      strings.Join(config.Databases, ","),
			"DB_USERS":      "app", // CNPG default user
		},
	}
	ctrl.SetControllerReference(db, configMap, r.Scheme)
	return configMap
}

func (r *DatabaseReconciler) generateSecret(ctx context.Context, db *enterprisev4.Database, cnpgCluster *cnpgv1.Cluster, config *enterprisev4.DatabaseConfig) (*corev1.Secret, error) {
	// Fetch CNPG app secret (contains the "app" user credentials)
	appSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-app", cnpgCluster.Name),
		Namespace: cnpgCluster.Namespace,
	}, appSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get CNPG app secret: %w", err)
	}

	// Build Secret Data - use app user credentials for all databases
	// Note: CNPG doesn't create separate superuser secrets by default
	secretData := map[string][]byte{
		"username": appSecret.Data["username"], // "app" user
		"password": appSecret.Data["password"], // app user password
	}

	// Add per-database passwords (all use the same app password from CNPG)
	for _, dbName := range config.Databases {
		key := fmt.Sprintf("%s_password", dbName)
		secretData[key] = appSecret.Data["password"]
	}

	// Create Secret object
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-credentials", db.Name),
			Namespace: db.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "database-controller",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(db, secret, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return secret, nil
}

func (r *DatabaseReconciler) reconcileConfigMap(ctx context.Context, db *enterprisev4.Database, cnpgCluster *cnpgv1.Cluster, config *enterprisev4.DatabaseConfig) error {
	logger := log.FromContext(ctx)

	configMap := r.generateConfigMap(db, cnpgCluster, config)
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("ConfigMap resource not found, creating one")
		if err := r.Create(ctx, configMap); err != nil {
			return err
		}
		if db.Status.Resources == nil {
			db.Status.Resources = &enterprisev4.DatabaseResources{}
		}
		db.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: configMap.Name}
		if err := r.Status().Update(ctx, db); err != nil {
			logger.Error(err, "Failed to update Database status with ConfigMap reference")
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to fetch existing configMap")
		return err
	}
	if existingConfigMap != nil {
		existingConfigMap.Data = configMap.Data
		if err := r.Update(ctx, existingConfigMap); err != nil {
			logger.Error(err, "Failed to update ConfigMap")
			return err
		}
	}
	return nil
}

func (r *DatabaseReconciler) reconcileSecret(ctx context.Context, db *enterprisev4.Database, cnpgCluster *cnpgv1.Cluster, config *enterprisev4.DatabaseConfig) error {
	logger := log.FromContext(ctx)

	// Generate the desired Secret
	secret, err := r.generateSecret(ctx, db, cnpgCluster, config)
	if err != nil {
		return fmt.Errorf("failed to generate secret: %w", err)
	}

	// Try to get existing Secret
	existingSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: secret.Namespace,
		Name:      secret.Name,
	}, existingSecret)

	if err != nil && errors.IsNotFound(err) {
		// Secret doesn't exist, create it
		logger.Info("Secret not found, creating", "name", secret.Name)
		if err := r.Create(ctx, secret); err != nil {
			logger.Error(err, "Failed to create Secret")
			return err
		}

		// Update Database status to reference the Secret
		if db.Status.Resources == nil {
			db.Status.Resources = &enterprisev4.DatabaseResources{}
		}
		db.Status.Resources.SecretRef = &corev1.LocalObjectReference{Name: secret.Name}

		if err := r.Status().Update(ctx, db); err != nil {
			logger.Error(err, "Failed to update Database status with Secret reference")
			return err
		}

		logger.Info("Secret created successfully", "name", secret.Name)
		return nil
	} else if err != nil {
		// Some other error occurred
		logger.Error(err, "Failed to get Secret")
		return err
	}

	// Secret exists, update its data
	logger.Info("Secret exists, updating data", "name", secret.Name)
	existingSecret.Data = secret.Data
	if err := r.Update(ctx, existingSecret); err != nil {
		logger.Error(err, "Failed to update Secret")
		return err
	}

	logger.Info("Secret updated successfully", "name", secret.Name)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.Database{}).
		Owns(&cnpgv1.Cluster{}).
		Owns(&cnpgv1.Database{}).
		Complete(r)
}
