// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrPostgresDatabasePending identifies absent, incomplete, or invalid producer
// data. Reconciliation retries without replacing an existing workload template.
var ErrPostgresDatabasePending = errors.New("PostgresDatabase dependency is not ready")

// ErrPostgresDatabaseInvalid identifies unsupported configuration or malformed
// published data. Callers report an error but keep recovery on future updates.
var ErrPostgresDatabaseInvalid = errors.New("PostgresDatabase dependency is invalid")

// ResolvePostgresDatabaseMounts follows same-namespace producer references,
// validates complete connection bundles, and returns no password or CA bytes.
// Phase 1 supports CNPG default TLS with provider and PostgresCluster names equal.
func ResolvePostgresDatabaseMounts(ctx context.Context, reader client.Reader, namespace string, specs []enterpriseApi.PostgresDatabaseMountSpec, volumes ...corev1.Volume) (resources.PostgresDatabaseBundle, error) {
	if err := ValidatePostgresDatabaseMounts(specs, volumes); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPostgresDatabaseInvalid, err)
	}
	var mounts resources.PostgresDatabaseBundle
	for _, spec := range specs {
		mount, err := resolvePostgresDatabaseMount(ctx, reader, namespace, spec)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

func resolvePostgresDatabaseMount(ctx context.Context, reader client.Reader, namespace string, spec enterpriseApi.PostgresDatabaseMountSpec) (resources.PostgresDatabaseMount, error) {
	var result resources.PostgresDatabaseMount
	db := &platformApi.PostgresDatabase{}
	if err := readPostgresResource(ctx, reader, namespace, spec.PostgresDatabaseRef.Name, db); err != nil {
		return result, err
	}
	if db.DeletionTimestamp != nil || db.Status.ObservedGeneration == nil || *db.Status.ObservedGeneration != db.Generation {
		return result, fmt.Errorf("%w: PostgresDatabase %s/%s has no current publication", ErrPostgresDatabasePending, namespace, db.Name)
	}
	declared := slices.ContainsFunc(db.Spec.Databases, func(d platformApi.DatabaseDefinition) bool { return d.Name == spec.Database })
	index := slices.IndexFunc(db.Status.Databases, func(d platformApi.DatabaseInfo) bool { return d.Name == spec.Database })
	if !declared {
		return result, fmt.Errorf("%w: database %q is not declared in PostgresDatabase %s/%s", ErrPostgresDatabaseInvalid, spec.Database, namespace, db.Name)
	}
	if index < 0 || !db.Status.Databases[index].Ready {
		return result, fmt.Errorf("%w: database %q in PostgresDatabase %s/%s is not ready", ErrPostgresDatabasePending, spec.Database, namespace, db.Name)
	}
	info := db.Status.Databases[index]
	if info.ConfigMapRef == nil || info.ConfigMapRef.Name == "" ||
		info.RWUserSecretRef == nil || info.RWUserSecretRef.Name == "" || info.RWUserSecretRef.Key == "" {
		return result, fmt.Errorf("%w: database %q has incomplete resource references", ErrPostgresDatabasePending, spec.Database)
	}
	if spec.IncludeAdmin && (info.AdminUserSecretRef == nil || info.AdminUserSecretRef.Name == "" || info.AdminUserSecretRef.Key == "") {
		return result, fmt.Errorf("%w: database %q has not published an admin credential reference", ErrPostgresDatabasePending, spec.Database)
	}
	cluster := &platformApi.PostgresCluster{}
	if err := readPostgresResource(ctx, reader, namespace, db.Spec.ClusterRef.Name, cluster); err != nil {
		return result, err
	}
	if cluster.DeletionTimestamp != nil || cluster.Status.ObservedGeneration == nil || *cluster.Status.ObservedGeneration != cluster.Generation ||
		cluster.Status.Phase == nil || *cluster.Status.Phase != "Ready" {
		return result, fmt.Errorf("%w: PostgresCluster %s/%s is not ready", ErrPostgresDatabasePending, namespace, cluster.Name)
	}
	if cluster.Status.ProvisionerRef != nil && cluster.Status.ProvisionerRef.Name != cluster.Name {
		return result, fmt.Errorf("%w: PostgresCluster %s/%s does not use the Phase 1 provider naming convention", ErrPostgresDatabaseInvalid, namespace, cluster.Name)
	}

	cm := &corev1.ConfigMap{}
	if err := readPostgresResource(ctx, reader, namespace, info.ConfigMapRef.Name, cm); err != nil {
		return result, err
	}
	if err := validatePostgresConnectionInfo(cm, spec.Database); err != nil {
		return result, err
	}
	// Secret payloads are used only for validation. They must never enter the
	// returned mount, rendered ConfigMap, status, events, or error messages.
	type credentialReference struct {
		ref  *corev1.SecretKeySelector
		user string
	}
	credentials := []credentialReference{{info.RWUserSecretRef, cm.Data["RW_USER_NAME"]}}
	if spec.IncludeAdmin {
		credentials = append(credentials, credentialReference{info.AdminUserSecretRef, cm.Data["ADMIN_USER_NAME"]})
	}
	for _, credential := range credentials {
		secret := &corev1.Secret{}
		if err := readPostgresResource(ctx, reader, namespace, credential.ref.Name, secret); err != nil {
			return result, err
		}
		if len(secret.Data[credential.ref.Key]) == 0 {
			return result, fmt.Errorf("%w: Secret %s/%s requires a non-empty password key %q", ErrPostgresDatabaseInvalid, namespace, secret.Name, credential.ref.Key)
		}
		if username, present := secret.Data["username"]; present && string(username) != credential.user {
			return result, fmt.Errorf("%w: Secret %s/%s username differs from published connection metadata", ErrPostgresDatabaseInvalid, namespace, secret.Name)
		}
	}
	// TODO(CPI-2235): Resolve the CA Secret reference from PostgresCluster status
	// once it exposes that reference, instead of deriving the name as <cluster>-ca.
	// Related CA discovery work: https://splunk.atlassian.net/browse/CPI-2235
	ca := &corev1.Secret{}
	if err := readPostgresResource(ctx, reader, namespace, cluster.Name+"-ca", ca); err != nil {
		return result, err
	}
	if len(ca.Data["ca.crt"]) == 0 {
		return result, fmt.Errorf("%w: Secret %s/%s has not published ca.crt", ErrPostgresDatabasePending, namespace, ca.Name)
	}
	if !containsPostgresCA(ca.Data["ca.crt"]) {
		return result, fmt.Errorf("%w: Secret %s/%s requires a PEM CA certificate in ca.crt", ErrPostgresDatabaseInvalid, namespace, ca.Name)
	}
	result = resources.PostgresDatabaseMount{
		Database:      spec.Database,
		ConfigMapName: cm.Name,
		RWPassword:    corev1.SecretKeySelector{Name: info.RWUserSecretRef.Name, Key: info.RWUserSecretRef.Key},
		CASecretName:  ca.Name,
	}
	if spec.IncludeAdmin {
		result.AdminPassword = &corev1.SecretKeySelector{Name: info.AdminUserSecretRef.Name, Key: info.AdminUserSecretRef.Key}
	}
	return result, nil
}

func readPostgresResource(ctx context.Context, reader client.Reader, namespace, name string, object client.Object) error {
	if name == "" {
		return fmt.Errorf("%w: a producer resource reference is empty", ErrPostgresDatabaseInvalid)
	}
	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := reader.Get(ctx, key, object); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: %T %s is missing", ErrPostgresDatabasePending, object, key)
		}
		return fmt.Errorf("read %T %s: %w", object, key, err)
	}
	if object.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%w: %T %s is terminating", ErrPostgresDatabasePending, object, key)
	}
	return nil
}

func validatePostgresConnectionInfo(cm *corev1.ConfigMap, database string) error {
	for _, key := range []string{"CLUSTER_RW_ENDPOINT", "DEFAULT_CLUSTER_PORT", "DATABASE_NAME", "ADMIN_USER_NAME", "RW_USER_NAME"} {
		if strings.TrimSpace(cm.Data[key]) == "" {
			return fmt.Errorf("%w: ConfigMap %s/%s requires non-empty %s", ErrPostgresDatabaseInvalid, cm.Namespace, cm.Name, key)
		}
	}
	if cm.Data["DATABASE_NAME"] != database {
		return fmt.Errorf("%w: ConfigMap %s/%s database differs from the selected database", ErrPostgresDatabaseInvalid, cm.Namespace, cm.Name)
	}
	host := cm.Data["CLUSTER_RW_ENDPOINT"]
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(host) != nil || len(validation.IsDNS1123Subdomain(host)) != 0 {
		return fmt.Errorf("%w: ConfigMap %s/%s requires a direct PostgreSQL DNS endpoint", ErrPostgresDatabaseInvalid, cm.Namespace, cm.Name)
	}
	port, err := strconv.Atoi(cm.Data["DEFAULT_CLUSTER_PORT"])
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%w: ConfigMap %s/%s has an invalid DEFAULT_CLUSTER_PORT", ErrPostgresDatabaseInvalid, cm.Namespace, cm.Name)
	}
	return nil
}

func containsPostgresCA(data []byte) bool {
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			return false
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err == nil && cert.IsCA {
			return true
		}
	}
	return false
}
