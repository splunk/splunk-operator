/*
Copyright 2026.

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
package core

import (
	"context"
	"errors"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=barmancloud.cnpg.io,resources=objectstores,verbs=get;list;watch;create;update;patch;delete

// ObjectStoreGVK is the GroupVersionKind of the barman-cloud ObjectStore CR the
// operator manages for object-storage backups. Exported so the controller (primary
// adapter) can set up an owner watch on it without redefining the GVK.
var ObjectStoreGVK = schema.GroupVersionKind{
	Group:   "barmancloud.cnpg.io",
	Version: "v1",
	Kind:    "ObjectStore",
}

type objectStoreModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	cluster      *platformv1alpha1.PostgresCluster
	mergedConfig *MergedConfig
}

func newObjectStoreModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, cluster *platformv1alpha1.PostgresCluster, mergedConfig *MergedConfig) *objectStoreModel {
	return &objectStoreModel{
		client:       c,
		scheme:       scheme,
		events:       events,
		updateStatus: updateStatus,
		cluster:      cluster,
		mergedConfig: mergedConfig,
	}
}

func (o *objectStoreModel) Name() string            { return pgcConstants.ComponentObjectStore }
func (o *objectStoreModel) Requires() []contractKey { return nil }
func (o *objectStoreModel) Provides() []contractKey { return nil }
func (o *objectStoreModel) CheckContracts() error   { return nil }

func (o *objectStoreModel) Reconcile(ctx context.Context) error {
	cfg := managedObjectStoreCfg(o.mergedConfig)
	if cfg == nil {
		if err := o.deleteObjectStore(ctx); err != nil {
			return newReconcileFailure(reasonObjectStoreReconcileFailed, err)
		}
		return nil
	}
	if err := o.createOrUpdateObjectStore(ctx, cfg); err != nil {
		return newReconcileFailure(reasonObjectStoreReconcileFailed, err)
	}
	return nil
}

func (o *objectStoreModel) Observe(_ context.Context, reconcileErr error) (componentHealth, error) {
	before := o.cluster.Status.DeepCopy()
	health, err := o.computeHealth(reconcileErr)
	statusErr := writeComponentStatus(o.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (o *objectStoreModel) computeHealth(reconcileErr error) (componentHealth, error) {
	if h, err, ok := classifyReconcileErr(reconcileErr, objectStoreReady, o.events, o.cluster, EventObjectStoreBackupReconcileFailed, "object store"); ok {
		return h, err
	}

	if managedObjectStoreCfg(o.mergedConfig) == nil {
		return newReadyHealth(objectStoreReady, reasonObjectStoreDisabled, "Object store backup not configured"), nil
	}

	return newReadyHealth(objectStoreReady, reasonObjectStoreConfigured, "ObjectStore is configured"), nil
}

func (o *objectStoreModel) createOrUpdateObjectStore(ctx context.Context, cfg *platformv1alpha1.CNPGBarmanObjectStoreConfig) error {
	name := objectStoreName(o.cluster.Name)
	desired := o.buildObjectStore(name, cfg)

	if err := ctrl.SetControllerReference(o.cluster, desired, o.scheme); err != nil {
		return fmt.Errorf("setting controller reference on ObjectStore: %w", err)
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(ObjectStoreGVK)
	err := o.client.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cluster.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := o.client.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("creating ObjectStore: %w", createErr)
		}
		o.events.emitNormal(o.cluster, EventObjectStoreCreated, fmt.Sprintf("ObjectStore %s created", name))
		return nil
	}
	if meta.IsNoMatchError(err) {
		return fmt.Errorf("barman-cloud plugin CRD not installed: %w", err)
	}
	if err != nil {
		return fmt.Errorf("getting ObjectStore: %w", err)
	}

	// Repair or verify our controller reference before mutating. SetControllerReference
	// returns *AlreadyOwnedError if a different controller owns this object, which guards
	// against mutating a foreign or orphaned ObjectStore that shares the deterministic name.
	original := existing.DeepCopy()
	if err := ctrl.SetControllerReference(o.cluster, existing, o.scheme); err != nil {
		return fmt.Errorf("repairing controller reference on ObjectStore: %w", err)
	}
	if err := unstructured.SetNestedField(existing.Object, desired.Object["spec"], "spec"); err != nil {
		return fmt.Errorf("setting ObjectStore spec for patch: %w", err)
	}
	if err := patchObject(ctx, o.client, original, existing, "ObjectStore"); err != nil {
		return fmt.Errorf("patching ObjectStore: %w", err)
	}
	return nil
}

func (o *objectStoreModel) deleteObjectStore(ctx context.Context) error {
	name := objectStoreName(o.cluster.Name)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ObjectStoreGVK)
	err := o.client.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cluster.Namespace}, obj)
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting ObjectStore for deletion: %w", err)
	}
	// Only delete an ObjectStore this PostgresCluster controls. A foreign or orphaned
	// object sharing the deterministic name must not be deleted by this controller.
	if controller := metav1.GetControllerOf(obj); controller == nil || controller.UID != o.cluster.UID {
		return nil
	}
	if err := o.client.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting ObjectStore: %w", err)
	}
	o.events.emitNormal(o.cluster, EventObjectStoreDeleted, fmt.Sprintf("ObjectStore %s deleted", name))
	return nil
}

// buildObjectStore assembles the desired barman-cloud ObjectStore as an
// unstructured object. The spec field names below mirror the
// barmancloud.cnpg.io/v1 ObjectStore CRD schema and are intentionally untyped:
// the barman-cloud plugin types are not vendored, so we cannot import the Go
// structs. If that CRD schema changes (field renames/restructures), these keys
// must be updated in lockstep — a mismatch produces an ObjectStore the plugin
// rejects rather than a compile error. See ObjectStoreGVK for the pinned version.
func (o *objectStoreModel) buildObjectStore(name string, cfg *platformv1alpha1.CNPGBarmanObjectStoreConfig) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ObjectStoreGVK)
	obj.SetName(name)
	obj.SetNamespace(o.cluster.Namespace)

	spec := map[string]interface{}{
		"configuration": map[string]interface{}{
			"destinationPath": cfg.DestinationPath,
			"s3Credentials": map[string]interface{}{
				"accessKeyId": map[string]interface{}{
					"name": cfg.S3Credentials.AccessKeyId.Name,
					"key":  cfg.S3Credentials.AccessKeyId.Key,
				},
				"secretAccessKey": map[string]interface{}{
					"name": cfg.S3Credentials.SecretAccessKey.Name,
					"key":  cfg.S3Credentials.SecretAccessKey.Key,
				},
			},
		},
	}

	if cfg.EndpointURL != nil {
		config := spec["configuration"].(map[string]interface{})
		config["endpointURL"] = *cfg.EndpointURL
	}
	if cfg.RetentionPolicy != nil {
		spec["retentionPolicy"] = *cfg.RetentionPolicy
	}
	if cfg.WAL != nil {
		wal := map[string]interface{}{}
		if cfg.WAL.Compression != nil {
			wal["compression"] = *cfg.WAL.Compression
		}
		if cfg.WAL.Encryption != nil {
			wal["encryption"] = *cfg.WAL.Encryption
		}
		if len(wal) > 0 {
			config := spec["configuration"].(map[string]interface{})
			config["wal"] = wal
		}
	}

	obj.Object["spec"] = spec
	return obj
}
