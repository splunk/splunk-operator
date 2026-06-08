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

	password "github.com/sethvargo/go-password/password"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type secretModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	cluster      *enterprisev4.PostgresCluster
	name         string
	contracts    *reconcileContracts
}

func newSecretModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, name string, contracts *reconcileContracts) *secretModel {
	return &secretModel{client: c, scheme: scheme, events: events, updateStatus: updateStatus, cluster: cluster, name: name, contracts: contracts}
}

func (s *secretModel) Name() string            { return pgcConstants.ComponentSecret }
func (s *secretModel) Requires() []contractKey { return nil }
func (s *secretModel) Provides() []contractKey { return []contractKey{contractSecret} }
func (s *secretModel) CheckContracts() error   { return nil }

func (s *secretModel) Reconcile(ctx context.Context) error {
	secret := &corev1.Secret{}
	secretExists, secretErr := clusterSecretExists(ctx, s.client, s.cluster.Namespace, s.name, secret)
	if secretErr != nil {
		return newReconcileFailure(reasonSuperUserSecretFailed, secretErr)
	}
	if !secretExists {
		var err error
		secret, err = ensureClusterSecret(ctx, s.client, s.scheme, s.cluster, s.name)
		if err != nil {
			return newReconcileFailure(reasonSuperUserSecretFailed, err)
		}
	}
	hasOwnerRef, ownerRefErr := controllerutil.HasOwnerReference(secret.GetOwnerReferences(), s.cluster, s.scheme)
	if ownerRefErr != nil {
		return newReconcileFailure(reasonSuperUserSecretFailed, fmt.Errorf("failed to check owner reference on secret: %w", ownerRefErr))
	}
	if secretExists && !hasOwnerRef {
		originalSecret := secret.DeepCopy()
		if err := ctrl.SetControllerReference(s.cluster, secret, s.scheme); err != nil {
			return newReconcileFailure(reasonSuperUserSecretFailed, fmt.Errorf("failed to set controller reference on existing secret: %w", err))
		}
		if err := patchObject(ctx, s.client, originalSecret, secret, "Secret"); err != nil {
			return newReconcileFailure(reasonSuperUserSecretFailed, err)
		}
		s.events.emitNormal(s.cluster, EventClusterAdopted, fmt.Sprintf("Adopted existing CNPG cluster and secret %s", s.name))
	}
	s.contracts.Secret = secret
	return nil
}

func (s *secretModel) Observe(_ context.Context, reconcileErr error) (componentHealth, error) {
	before := s.cluster.Status.DeepCopy()
	health, err := s.computeHealth(reconcileErr)
	statusErr := writeComponentStatus(s.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (s *secretModel) computeHealth(reconcileErr error) (componentHealth, error) {
	if rf, ok := errors.AsType[*reconcileFailure](reconcileErr); ok {
		s.events.emitWarning(s.cluster, EventSecretReconcileFailed, fmt.Sprintf("failed to reconcile superuser secret for PostgresCluster %s — check operator logs", s.cluster.Name))
		return newFailedHealth(secretsReady, rf.reason, rf.err.Error()), rf.err
	}

	secret := s.contracts.Secret
	if s.cluster.Status.Resources.SuperUserSecretRef == nil {
		s.cluster.Status.Resources.SuperUserSecretRef = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: s.name},
			Key:                  secretKeyPassword,
		}
	}

	refKey := s.cluster.Status.Resources.SuperUserSecretRef.Key
	if refKey == "" {
		refKey = secretKeyPassword
	}
	if _, ok := secret.Data[refKey]; !ok {
		return newFailedHealth(secretsReady, reasonSuperUserSecretFailed, fmt.Sprintf(msgFmtSecretMissingKey, refKey)),
			fmt.Errorf("secret missing key %s", refKey)
	}

	h := newReadyHealth(secretsReady, reasonSuperUserSecretReady, msgSuperuserSecretReady)
	if !meta.IsStatusConditionTrue(s.cluster.Status.Conditions, string(secretsReady)) {
		s.events.emitNormal(s.cluster, EventSecretReady, h.Message)
	}
	return h, nil
}

// ensureClusterSecret creates the superuser secret. Caller must verify it does not already exist.
func ensureClusterSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, secretName string) (*corev1.Secret, error) {
	pw, err := generatePassword()
	if err != nil {
		return nil, err
	}
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: cluster.Namespace},
		StringData: map[string]string{"username": superUsername, "password": pw},
		Type:       corev1.SecretTypeOpaque,
	}
	if err := ctrl.SetControllerReference(cluster, newSecret, scheme); err != nil {
		return nil, err
	}
	if err := c.Create(ctx, newSecret); err != nil {
		return nil, err
	}
	return newSecret, nil
}

func clusterSecretExists(ctx context.Context, c client.Client, namespace, name string, secret *corev1.Secret) (bool, error) {
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func generatePassword() (string, error) {
	const (
		length  = 32
		digits  = 8
		symbols = 0
	)
	return password.Generate(length, digits, symbols, false, true)
}
