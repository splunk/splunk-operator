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
package k8s

import (
	"context"
	"fmt"

	password "github.com/sethvargo/go-password/password"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// Keep database-generated credentials aligned with the existing
	// PostgresCluster password policy (32 characters, 8 digits, no symbols).
	// A separate constant set makes this database policy explicit at its only
	// generation boundary.
	generatedPasswordLength  = 32
	generatedPasswordDigits  = 8
	generatedPasswordSymbols = 0

	secretKeyPassword      = "password"
	secretKeyUsername      = "username"
	labelManagedBy         = "app.kubernetes.io/managed-by"
	labelCNPGReload        = "cnpg.io/reload"
	annotationRetainedFrom = "platform.splunk.com/retained-from"
)

// SecretSnapshot is the Kubernetes-shaped, non-sensitive result of one Secret
// read. The database adapter translates it into credential policy facts.
// Password bytes never leave this infrastructure package.
type SecretSnapshot struct {
	DataDefined     bool
	Username        string
	UsernamePresent bool
	PasswordPresent bool
	ReloadEnabled   bool
	RetainedFrom    string
	ResourceVersion string
	Controller      *metav1.OwnerReference
}

// SecretStore performs the Kubernetes operations needed by database role
// credentials. It is bound to the PostgresDatabase that owns generated
// credentials, so the core port never exposes generic owner mutations.
type SecretStore struct {
	client client.Client
	scheme *runtime.Scheme
	owner  *platformv1alpha1.PostgresDatabase
}

// NewSecretStore returns an owner-bound Kubernetes Secret store.
func NewSecretStore(c client.Client, scheme *runtime.Scheme, owner *platformv1alpha1.PostgresDatabase) SecretStore {
	return SecretStore{client: c, scheme: scheme, owner: owner}
}

// Read returns a non-sensitive Kubernetes Secret snapshot. Password bytes
// remain inside the Kubernetes implementation.
func (s SecretStore) Read(ctx context.Context, namespace, name string) (SecretSnapshot, error) {
	secret := &corev1.Secret{}
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return SecretSnapshot{}, err
	}
	return secretSnapshot(secret), nil
}

func secretSnapshot(secret *corev1.Secret) SecretSnapshot {
	username := secret.Data[secretKeyUsername]
	password := secret.Data[secretKeyPassword]
	snapshot := SecretSnapshot{
		DataDefined:     secret.Data != nil,
		Username:        string(username),
		UsernamePresent: len(username) > 0,
		PasswordPresent: len(password) > 0,
		ReloadEnabled:   secret.Labels[labelCNPGReload] == "true",
		RetainedFrom:    secret.Annotations[annotationRetainedFrom],
		ResourceVersion: secret.ResourceVersion,
	}
	if controller := metav1.GetControllerOf(secret); controller != nil {
		controllerCopy := *controller
		snapshot.Controller = &controllerCopy
	}
	return snapshot
}

// CreateGenerated creates the only Secret path that carries generated password
// material. Existing Secret data is never modified elsewhere in this store.
func (s SecretStore) CreateGenerated(ctx context.Context, namespace, name, username string) error {
	if s.owner == nil {
		return fmt.Errorf("PostgresDatabase owner is not configured")
	}
	generatedPassword, err := password.Generate(generatedPasswordLength, generatedPasswordDigits, generatedPasswordSymbols, false, true)
	if err != nil {
		return fmt.Errorf("generating credential password: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy:  "splunk-operator",
				labelCNPGReload: "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			secretKeyUsername: []byte(username),
			secretKeyPassword: []byte(generatedPassword),
		},
	}
	if err := controllerutil.SetControllerReference(s.owner, secret, s.scheme); err != nil {
		return fmt.Errorf("setting owner reference on Secret %s: %w", name, err)
	}
	return s.client.Create(ctx, secret)
}

// Adopt changes only Secret metadata. It removes the retention annotation and
// restores the database controller reference without changing credential data.
// The expected resource version prevents a controller change after policy read
// from being misclassified as an ownership or credential failure.
func (s SecretStore) Adopt(ctx context.Context, namespace, name, expectedResourceVersion string) error {
	if s.owner == nil {
		return fmt.Errorf("PostgresDatabase owner is not configured")
	}
	secret := &corev1.Secret{}
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return err
	}
	if expectedResourceVersion != "" && secret.ResourceVersion != expectedResourceVersion {
		return apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, name,
			fmt.Errorf("Secret resource version changed from %q to %q", expectedResourceVersion, secret.ResourceVersion))
	}
	if secret.Annotations != nil {
		delete(secret.Annotations, annotationRetainedFrom)
	}
	if err := controllerutil.SetControllerReference(s.owner, secret, s.scheme); err != nil {
		return fmt.Errorf("setting owner reference on Secret %s: %w", name, err)
	}
	return s.client.Update(ctx, secret)
}
