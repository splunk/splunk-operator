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

package config

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

// NoahAuthSecretKey is the Secret data key containing Noah's pass4SymmKey.
const NoahAuthSecretKey = "pass4SymmKey"

// NoahDependencyErrorKind classifies configuration failures that reconcilers map
// to their own phases, conditions, events, and requeue behavior.
type NoahDependencyErrorKind string

const (
	// NoahDependencyMissing means a referenced Kubernetes dependency is absent.
	NoahDependencyMissing NoahDependencyErrorKind = "missing"
	// NoahDependencyInvalid means a dependency exists but cannot configure Noah.
	NoahDependencyInvalid NoahDependencyErrorKind = "invalid"
)

// NoahDependencyError reports a missing or invalid Noah dependency.
type NoahDependencyError struct {
	kind NoahDependencyErrorKind
	err  error
}

// Error implements error.
func (err *NoahDependencyError) Error() string {
	if err == nil || err.err == nil {
		return "Noah dependency error"
	}
	return err.err.Error()
}

// Unwrap returns the underlying validation or Kubernetes error.
func (err *NoahDependencyError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.err
}

// Kind returns the dependency failure classification.
func (err *NoahDependencyError) Kind() NoahDependencyErrorKind {
	if err == nil {
		return ""
	}
	return err.kind
}

// NoahRuntime contains validated Noah connection inputs. Client construction is
// lazy so reconcilers that only render configuration do not derive an HMAC key.
type NoahRuntime struct {
	spec       enterpriseApi.NoahClusterSpec
	credential []byte
	client     *noahclient.Client
}

// ResolveNoahRuntime resolves and validates a same-namespace NoahCluster and
// its authentication Secret without constructing an authenticated client.
func ResolveNoahRuntime(ctx context.Context, reader k8sclient.Reader, namespace string, ref corev1.LocalObjectReference) (*NoahRuntime, error) {
	if ref.Name == "" {
		return nil, noahDependencyError(NoahDependencyInvalid, fmt.Errorf("noahClusterRef.name must not be empty"))
	}

	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	cluster := &enterpriseApi.NoahCluster{}
	if err := reader.Get(ctx, key, cluster); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, noahDependencyError(NoahDependencyMissing, fmt.Errorf("get referenced NoahCluster %s: %w", key, err))
		}
		return nil, fmt.Errorf("get referenced NoahCluster %s: %w", key, err)
	}

	credential, err := resolveNoahAuthSecret(ctx, reader, namespace, cluster.Spec.AuthSecretRef)
	if err != nil {
		return nil, err
	}

	return &NoahRuntime{
		spec:       cluster.DeepCopy().Spec,
		credential: append([]byte(nil), credential...),
	}, nil
}

// Spec returns a copy of the resolved NoahCluster specification.
func (runtime *NoahRuntime) Spec() enterpriseApi.NoahClusterSpec {
	return *runtime.spec.DeepCopy()
}

// Credential returns a copy of the validated pass4SymmKey. The returned value
// is sensitive and must only be used for Noah client construction or
// Secret-backed workload provisioning. It must never be logged or written to
// configuration maps, status, events, or command arguments.
func (runtime *NoahRuntime) Credential() []byte {
	return append([]byte(nil), runtime.credential...)
}

// Client constructs and caches an authenticated Noah API client.
func (runtime *NoahRuntime) Client() (*noahclient.Client, error) {
	if runtime.client != nil {
		return runtime.client, nil
	}

	authenticator, err := noahclient.NewHMACV2Authenticator(runtime.credential)
	if err != nil {
		return nil, noahDependencyError(NoahDependencyInvalid, fmt.Errorf("configure Noah authentication: %w", err))
	}

	runtime.client, err = noahclient.NewClient(runtime.spec.Endpoint, runtime.spec.Tenant, authenticator)
	if err != nil {
		return nil, noahDependencyError(NoahDependencyInvalid, fmt.Errorf("configure Noah client: %w", err))
	}

	return runtime.client, nil
}

func resolveNoahAuthSecret(ctx context.Context, reader k8sclient.Reader, namespace string, ref corev1.LocalObjectReference) ([]byte, error) {
	if ref.Name == "" {
		return nil, noahDependencyError(NoahDependencyInvalid, fmt.Errorf("authSecretRef.name must not be empty"))
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	if err := reader.Get(ctx, key, secret); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, noahDependencyError(NoahDependencyMissing, fmt.Errorf("get Noah auth Secret %s: %w", key, err))
		}
		return nil, fmt.Errorf("get Noah auth Secret %s: %w", key, err)
	}
	credential, found := secret.Data[NoahAuthSecretKey]
	if !found {
		return nil, noahDependencyError(NoahDependencyInvalid, fmt.Errorf("Noah auth Secret %s is missing data.%s", key, NoahAuthSecretKey))
	}
	if err := splutil.ValidateSecret(credential); err != nil {
		return nil, noahDependencyError(NoahDependencyInvalid, fmt.Errorf("Noah auth Secret %s has invalid data.%s: %w", key, NoahAuthSecretKey, err))
	}
	if strings.ContainsAny(string(credential), "\r\n") {
		return nil, noahDependencyError(NoahDependencyInvalid, fmt.Errorf("Noah auth Secret %s data.%s must be a single line", key, NoahAuthSecretKey))
	}
	return credential, nil
}

func noahDependencyError(kind NoahDependencyErrorKind, err error) error {
	return &NoahDependencyError{kind: kind, err: err}
}
