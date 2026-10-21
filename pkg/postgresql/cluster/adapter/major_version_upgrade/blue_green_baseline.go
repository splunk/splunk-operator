/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package majorupgradeadapter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type sourceBaseline struct {
	client              client.Client
	key                 types.NamespacedName
	cluster             *platformv1alpha1.PostgresCluster
	targetConfiguration *platformv1alpha1.PostgresClusterSpec
	targetImage         string
}

type frozenDatabaseDeclaration struct {
	Name       string                              `json:"name"`
	UID        types.UID                           `json:"uid"`
	Definition platformv1alpha1.DatabaseDefinition `json:"definition"`
}

func (p sourceBaseline) secretFingerprint(ctx context.Context, name string) (platformv1alpha1.BlueGreenCredentialFingerprintStatus, error) {
	secret := &corev1.Secret{}
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: name}, secret); err != nil {
		return platformv1alpha1.BlueGreenCredentialFingerprintStatus{}, err
	}
	digest := sha256.New()
	keys := make([]string, 0, len(secret.Data))
	for key := range secret.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		digest.Write([]byte(key))
		digest.Write([]byte{0})
		digest.Write(secret.Data[key])
		digest.Write([]byte{0})
	}
	now := metav1.Now()
	return platformv1alpha1.BlueGreenCredentialFingerprintStatus{SecretName: secret.Name, SecretUID: string(secret.UID), Fingerprint: fmt.Sprintf("%x", digest.Sum(nil)), CapturedAt: &now, VerifiedAt: &now}, nil
}

func (p sourceBaseline) credentialFingerprints(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, replicationSecret string) ([]platformv1alpha1.BlueGreenCredentialFingerprintStatus, error) {
	ordered := credentialSecretNames(source, databases, replicationSecret)
	result := make([]platformv1alpha1.BlueGreenCredentialFingerprintStatus, 0, len(ordered))
	for _, name := range ordered {
		fingerprint, err := p.secretFingerprint(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("fingerprinting credential Secret %q: %w", name, err)
		}
		result = append(result, fingerprint)
	}
	return result, nil
}

func credentialSecretNames(source *cnpgv1.Cluster, databases []declaredDatabase, replicationSecret string) []string {
	names := map[string]struct{}{replicationSecret: {}}
	if source.GetSuperuserSecretName() != "" {
		names[source.GetSuperuserSecretName()] = struct{}{}
	}
	if source.Status.Certificates.ServerCASecret != "" {
		names[source.Status.Certificates.ServerCASecret] = struct{}{}
	}
	for _, database := range databases {
		addDatabaseCredentialSecretNames(names, database)
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		if name != "" {
			ordered = append(ordered, name)
		}
	}
	sort.Strings(ordered)
	return ordered
}

func addDatabaseCredentialSecretNames(names map[string]struct{}, database declaredDatabase) {
	if config := database.definition.PasswordConfig; config != nil {
		names[config.ExternalAdminSecretRef.Name] = struct{}{}
		names[config.ExternalRWSecretRef.Name] = struct{}{}
	}
	for _, info := range database.parent.Status.Databases {
		if info.Name != database.name {
			continue
		}
		if info.AdminUserSecretRef != nil {
			names[info.AdminUserSecretRef.Name] = struct{}{}
		}
		if info.RWUserSecretRef != nil {
			names[info.RWUserSecretRef.Name] = struct{}{}
		}
	}
}

func (p sourceBaseline) configurationFingerprint(source *cnpgv1.Cluster, databases []declaredDatabase) (string, error) {
	if p.cluster == nil || source == nil {
		return "", fmt.Errorf("%w: source configuration is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	cnpgSpec := *source.Spec.DeepCopy()
	declarations := make([]frozenDatabaseDeclaration, 0, len(databases))
	for _, database := range databases {
		declarations = append(declarations, frozenDatabaseDeclaration{Name: database.parent.Name, UID: database.parent.UID, Definition: database.definition})
	}
	baseline := struct {
		Cluster   platformv1alpha1.PostgresClusterSpec  `json:"cluster"`
		CNPG      clustercore.BlueGreenSourceCNPGSpec   `json:"cnpg"`
		Databases []frozenDatabaseDeclaration           `json:"databases"`
		Target    *platformv1alpha1.PostgresClusterSpec `json:"target"`
		Image     string                                `json:"image"`
	}{
		Cluster:   mvutypes.FrozenBlueGreenClusterSpec(p.cluster.Spec),
		CNPG:      clustercore.NormalizeBlueGreenSourceCNPGSpec(cnpgSpec),
		Databases: declarations,
		Image:     p.targetImage,
	}
	if p.targetConfiguration != nil {
		target := mvutypes.FrozenBlueGreenClusterSpec(*p.targetConfiguration)
		baseline.Target = &target
	}
	encoded, err := json.Marshal(baseline)
	if err != nil {
		return "", fmt.Errorf("normalizing source configuration: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:]), nil
}
