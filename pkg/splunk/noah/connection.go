// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package noah

import (
	"context"
	"fmt"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// AuthSecretKey is the Secret data key containing Noah's pass4SymmKey.
const AuthSecretKey = "pass4SymmKey"

// DependencyErrorKind classifies configuration failures that reconcilers map
// to their own phases, conditions, events, and requeue behavior.
type DependencyErrorKind string

const (
	// DependencyMissing means a referenced Kubernetes dependency is absent.
	DependencyMissing DependencyErrorKind = "missing"
	// DependencyInvalid means a dependency exists but cannot configure Noah.
	DependencyInvalid DependencyErrorKind = "invalid"
)

// DependencyError reports a missing or invalid Noah dependency.
type DependencyError struct {
	kind DependencyErrorKind
	err  error
}

// Error implements error.
func (err *DependencyError) Error() string {
	if err == nil || err.err == nil {
		return "Noah dependency error"
	}
	return err.err.Error()
}

// Unwrap returns the underlying validation or Kubernetes error.
func (err *DependencyError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.err
}

// Kind returns the dependency failure classification.
func (err *DependencyError) Kind() DependencyErrorKind {
	if err == nil {
		return ""
	}
	return err.kind
}

// Connection contains validated Noah connection inputs. Client construction is
// lazy so reconcilers that only render configuration do not derive an HMAC key.
type Connection struct {
	spec                      enterpriseApi.NoahClusterSpec
	authSecretName            string
	authSecretResourceVersion string
	credential                []byte
	client                    *noahclient.Client
}

// ResolveConnection resolves and validates a same-namespace NoahCluster and
// its authentication Secret without constructing an authenticated client.
func ResolveConnection(ctx context.Context, reader k8sclient.Reader, namespace string, ref corev1.LocalObjectReference) (*Connection, error) {
	if ref.Name == "" {
		return nil, dependencyError(DependencyInvalid, fmt.Errorf("noahClusterRef.name must not be empty"))
	}

	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	cluster := &enterpriseApi.NoahCluster{}
	if err := reader.Get(ctx, key, cluster); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, dependencyError(DependencyMissing, fmt.Errorf("get referenced NoahCluster %s: %w", key, err))
		}
		return nil, fmt.Errorf("get referenced NoahCluster %s: %w", key, err)
	}

	authSecretName, authSecretResourceVersion, credential, err := resolveAuthSecret(ctx, reader, namespace, cluster.Spec.AuthSecretRef)
	if err != nil {
		return nil, err
	}

	return &Connection{
		spec:                      cluster.DeepCopy().Spec,
		authSecretName:            authSecretName,
		authSecretResourceVersion: authSecretResourceVersion,
		credential:                append([]byte(nil), credential...),
	}, nil
}

// Spec returns a copy of the resolved NoahCluster specification.
func (connection *Connection) Spec() enterpriseApi.NoahClusterSpec {
	return *connection.spec.DeepCopy()
}

// AuthSecretName returns the resolved credential Secret name.
func (connection *Connection) AuthSecretName() string {
	return connection.authSecretName
}

// Credential returns a copy of the validated pass4SymmKey. The returned value
// is sensitive and must only be used for Noah client construction or
// Secret-backed workload provisioning. It must never be logged or written to
// configuration maps, status, events, or command arguments.
func (connection *Connection) Credential() []byte {
	return append([]byte(nil), connection.credential...)
}

// AuthSecretResourceVersion returns the opaque Kubernetes revision of the
// resolved credential Secret.
// TODO(CSPL-5241): Replace ResourceVersion with a secure, opaque content revision.
func (connection *Connection) AuthSecretResourceVersion() string {
	return connection.authSecretResourceVersion
}

// Client constructs and caches an authenticated Noah API client.
func (connection *Connection) Client() (*noahclient.Client, error) {
	if connection.client != nil {
		return connection.client, nil
	}

	authenticator, err := noahclient.NewHMACV2Authenticator(connection.credential)
	if err != nil {
		return nil, dependencyError(DependencyInvalid, fmt.Errorf("configure Noah authentication: %w", err))
	}

	connection.client, err = noahclient.NewClient(connection.spec.Endpoint, connection.spec.Tenant, authenticator)
	if err != nil {
		return nil, dependencyError(DependencyInvalid, fmt.Errorf("configure Noah client: %w", err))
	}

	return connection.client, nil
}

func resolveAuthSecret(ctx context.Context, reader k8sclient.Reader, namespace string, ref corev1.LocalObjectReference) (string, string, []byte, error) {
	if ref.Name == "" {
		return "", "", nil, dependencyError(DependencyInvalid, fmt.Errorf("authSecretRef.name must not be empty"))
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	if err := reader.Get(ctx, key, secret); err != nil {
		if k8serrors.IsNotFound(err) {
			return "", "", nil, dependencyError(DependencyMissing, fmt.Errorf("get Noah auth Secret %s: %w", key, err))
		}
		return "", "", nil, fmt.Errorf("get Noah auth Secret %s: %w", key, err)
	}
	credential, found := secret.Data[AuthSecretKey]
	if !found {
		return "", "", nil, dependencyError(DependencyInvalid, fmt.Errorf("Noah auth Secret %s is missing data.%s", key, AuthSecretKey))
	}
	if err := splutil.ValidateSecret(credential); err != nil {
		return "", "", nil, dependencyError(DependencyInvalid, fmt.Errorf("Noah auth Secret %s has invalid data.%s: %w", key, AuthSecretKey, err))
	}
	if strings.ContainsAny(string(credential), "\r\n") {
		return "", "", nil, dependencyError(DependencyInvalid, fmt.Errorf("Noah auth Secret %s data.%s must be a single line", key, AuthSecretKey))
	}
	return secret.Name, secret.ResourceVersion, credential, nil
}

func dependencyError(kind DependencyErrorKind, err error) error {
	return &DependencyError{kind: kind, err: err}
}
