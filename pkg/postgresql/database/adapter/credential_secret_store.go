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
package adapter

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbcredentials "github.com/splunk/splunk-operator/pkg/postgresql/database/core/components/credentials"
	dbk8s "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type credentialSecretStore struct {
	store dbk8s.SecretStore
	owner dbcredentials.OwnerIdentity
}

// NewCredentialSecretOperations returns the database adapter for Secret policy
// operations. The adapter is bound to the supplied PostgresDatabase owner.
func NewCredentialSecretOperations(c client.Client, scheme *runtime.Scheme, owner *platformv1alpha1.PostgresDatabase) dbcredentials.SecretOperations {
	return &credentialSecretStore{
		store: dbk8s.NewSecretStore(c, scheme, owner),
		owner: credentialOwnerIdentity(owner),
	}
}

func (s *credentialSecretStore) OwnerIdentity() dbcredentials.OwnerIdentity { return s.owner }

func credentialOwnerIdentity(owner *platformv1alpha1.PostgresDatabase) dbcredentials.OwnerIdentity {
	if owner == nil {
		return dbcredentials.OwnerIdentity{}
	}
	return dbcredentials.OwnerIdentity{Name: owner.Name, UID: string(owner.UID), Kind: owner.Kind}
}

func (s *credentialSecretStore) Read(ctx context.Context, ref dbcredentials.SecretRef) (dbcredentials.ObservedSecret, error) {
	snapshot, err := s.store.Read(ctx, ref.Namespace, ref.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return dbcredentials.ObservedSecret{}, fmt.Errorf("%w: %w", dbcredentials.ErrSecretNotFound, err)
		}
		return dbcredentials.ObservedSecret{}, err
	}
	return observedSecret(snapshot), nil
}

// observedSecret is the single Kubernetes snapshot-to-policy-facts conversion.
// It intentionally excludes password material from the core contract.
func observedSecret(snapshot dbk8s.SecretSnapshot) dbcredentials.ObservedSecret {
	facts := dbcredentials.ObservedSecret{
		DataDefined:     snapshot.DataDefined,
		Username:        snapshot.Username,
		UsernamePresent: snapshot.UsernamePresent,
		PasswordPresent: snapshot.PasswordPresent,
		ReloadEnabled:   snapshot.ReloadEnabled,
		RetainedFrom:    snapshot.RetainedFrom,
		ResourceVersion: snapshot.ResourceVersion,
	}
	if snapshot.Controller != nil {
		facts.Controller = &dbcredentials.OwnerIdentity{
			Name: snapshot.Controller.Name,
			UID:  string(snapshot.Controller.UID),
			Kind: snapshot.Controller.Kind,
		}
	}
	return facts
}

func (s *credentialSecretStore) CreateGenerated(ctx context.Context, ref dbcredentials.SecretRef, username string) error {
	err := s.store.CreateGenerated(ctx, ref.Namespace, ref.Name, username)
	if apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("%w: %w", dbcredentials.ErrSecretAlreadyExists, err)
	}
	return err
}

func (s *credentialSecretStore) Adopt(ctx context.Context, ref dbcredentials.SecretRef, resourceVersion string) error {
	err := s.store.Adopt(ctx, ref.Namespace, ref.Name, resourceVersion)
	if apierrors.IsConflict(err) {
		return fmt.Errorf("%w: %w", dbcredentials.ErrSecretConflict, err)
	}
	return err
}
