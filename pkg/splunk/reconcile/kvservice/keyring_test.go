// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestTenantKeysRotation(t *testing.T) {
	tests := []struct {
		name    string
		current tenantKeys
		next    string
		want    tenantKeys
		invalid bool
	}{
		{"initial", tenantKeys{}, "first", tenantKeys{current: "first"}, false},
		{"unchanged", tenantKeys{current: "first", previous: "old"}, "first", tenantKeys{current: "first", previous: "old"}, false},
		{"rotation", tenantKeys{current: "first"}, "second", tenantKeys{current: "second", previous: "first"}, false},
		{"third key", tenantKeys{current: "second", previous: "first"}, "third", tenantKeys{current: "second", previous: "first"}, true},
		{"after retirement", tenantKeys{current: "second"}, "third", tenantKeys{current: "third", previous: "second"}, false},
		{"empty source", tenantKeys{current: "first"}, "", tenantKeys{current: "first"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.current.rotate(test.next)
			if test.invalid {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "first")
				require.NotContains(t, err.Error(), "second")
				require.NotContains(t, err.Error(), "third")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func TestParseTenantKeys(t *testing.T) {
	for _, raw := range []string{`not-json`, `null`, `[]`, `[""]`, `["one",""]`, `["one","two","three"]`} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseTenantKeys([]byte(raw))
			require.Error(t, err)
			require.ErrorContains(t, err, "invalid keyring entry")
		})
	}
	for _, test := range []struct {
		raw  string
		want tenantKeys
	}{
		{"", tenantKeys{}}, {`["one"]`, tenantKeys{current: "one"}},
		{`["two","one"]`, tenantKeys{current: "two", previous: "one"}},
		{`["one","one"]`, tenantKeys{current: "one"}},
	} {
		got, err := parseTenantKeys([]byte(test.raw))
		require.NoError(t, err)
		require.Equal(t, test.want, got)
	}
}

func changeTenantSource(t *testing.T, ctx context.Context, c client.Client, namespace, value string) {
	t.Helper()
	source := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: splcommon.GetNamespaceScopedSecretName(namespace)}, source))
	source.Data["pass4SymmKey"] = []byte(value)
	require.NoError(t, c.Update(ctx, source))
}

func TestTenantKeyringPublishesRotationAndRevocationOnceWithUnreadableSource(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			ctx, cr, c := tenantFixture(t)
			mapping := map[string]string{"alpha": "ns-a", "bravo": "ns-b"}
			ring, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
			require.NoError(t, err)
			retained := append([]byte(nil), ring.Data["bravo"]...)
			ring.Data["removed"] = []byte(`["removed-key"]`)
			require.NoError(t, c.Update(ctx, ring))
			changeTenantSource(t, ctx, c, "ns-a", "alpha-new")
			readErr := errors.New("temporary read failure")
			if missing {
				readErr = apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "splunk-ns-b-secret")
			}
			writes := 0
			guarded := interceptor.NewClient(c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Namespace == "ns-b" {
						return readErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if obj.GetName() == ring.Name {
						writes++
					}
					return c.Update(ctx, obj, opts...)
				},
			})
			updated, err := reconcileKVServiceTenantKeys(ctx, guarded, cr, mapping)
			require.ErrorIs(t, err, readErr)
			require.Equal(t, missing, onlyMissingTenantSecrets(err))
			require.Equal(t, 1, writes)
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(ring), ring))
			require.Equal(t, updated.Data, ring.Data)
			require.Equal(t, retained, ring.Data["bravo"])
			require.NotContains(t, ring.Data, "removed")
			var keys []string
			require.NoError(t, json.Unmarshal(ring.Data["alpha"], &keys))
			require.Equal(t, []string{"alpha-new", "alpha-key"}, keys)
			_, err = reconcileKVServiceTenantKeys(ctx, guarded, cr, mapping)
			require.ErrorIs(t, err, readErr)
			require.Equal(t, 1, writes, "an unchanged reconcile must not publish again")
		})
	}
}

func TestTenantKeyringAggregatesSourceFailures(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	missing := apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "source")
	unavailable := errors.New("API unavailable")
	guarded := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if key.Namespace == "ns-a" {
			return missing
		}
		if key.Namespace == "ns-b" {
			return unavailable
		}
		return c.Get(ctx, key, obj, opts...)
	}})
	_, err := reconcileKVServiceTenantKeys(ctx, guarded, cr, map[string]string{"alpha": "ns-a", "bravo": "ns-b"})
	require.ErrorIs(t, err, missing)
	require.ErrorIs(t, err, unavailable)
	require.ErrorContains(t, err, `tenant "alpha"`)
	require.ErrorContains(t, err, `tenant "bravo"`)
	require.False(t, onlyMissingTenantSecrets(err))
}

func TestTenantKeyringAllowsSharedRetainedKeyAndRevokesRemovedTenant(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	mapping := map[string]string{"alpha": "ns-a", "bravo": "ns-b"}
	ring, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	original := append([]byte(nil), ring.Data["bravo"]...)
	ring.Data["removed"] = []byte(`["removed-key"]`)
	require.NoError(t, c.Update(ctx, ring))
	changeTenantSource(t, ctx, c, "ns-a", "bravo-key")
	source := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "ns-b", Name: "splunk-ns-b-secret"}, source))
	require.NoError(t, c.Delete(ctx, source))
	writes := 0
	guarded := interceptor.NewClient(c, interceptor.Funcs{Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
		writes++
		return c.Update(ctx, obj, opts...)
	}})
	_, err = reconcileKVServiceTenantKeys(ctx, guarded, cr, mapping)
	require.True(t, apierrors.IsNotFound(err))
	require.True(t, onlyMissingTenantSecrets(err), "a shared key must not introduce an additional validation failure")
	_, terminal := splcommon.TerminalMessage(err)
	require.False(t, terminal)
	require.Equal(t, 1, writes)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(ring), ring))
	require.Equal(t, []byte(`["bravo-key","alpha-key"]`), ring.Data["alpha"])
	require.Equal(t, original, ring.Data["bravo"])
	require.NotContains(t, ring.Data, "removed")
	require.NotContains(t, err.Error(), "bravo-key")
}

func TestTenantKeyringInvalidRotationDoesNotBlockHealthyTenant(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	mapping := map[string]string{"alpha": "ns-a", "bravo": "ns-b"}
	_, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	changeTenantSource(t, ctx, c, "ns-a", "alpha-new")
	ring, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	retained := append([]byte(nil), ring.Data["alpha"]...)
	changeTenantSource(t, ctx, c, "ns-a", "alpha-third")
	changeTenantSource(t, ctx, c, "ns-b", "bravo-new")
	ring, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.ErrorContains(t, err, "unretired")
	require.Equal(t, retained, ring.Data["alpha"])
	var keys []string
	require.NoError(t, json.Unmarshal(ring.Data["bravo"], &keys))
	require.Equal(t, []string{"bravo-new", "bravo-key"}, keys)
}

func TestTenantKeyringWriteFailureRemainsRetryableWithInvalidSource(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	mapping := map[string]string{"alpha": "ns-a", "bravo": "ns-b"}
	ring, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	before := ring.DeepCopy()
	changeTenantSource(t, ctx, c, "ns-a", "")
	changeTenantSource(t, ctx, c, "ns-b", "bravo-new")
	writeErr := errors.New("temporary write failure")
	guarded := interceptor.NewClient(c, interceptor.Funcs{Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
		return writeErr
	}})
	_, err = reconcileKVServiceTenantKeys(ctx, guarded, cr, mapping)
	require.ErrorIs(t, err, writeErr)
	_, terminal := splcommon.TerminalMessage(err)
	require.False(t, terminal)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(ring), ring))
	require.Equal(t, before.Data, ring.Data)
	ring, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	message, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	require.Contains(t, message, "ns-a/splunk-ns-a-secret")
	require.Equal(t, []byte(`["bravo-new","bravo-key"]`), ring.Data["bravo"])
}

func TestOnlyMissingTenantSecrets(t *testing.T) {
	missing := apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "source")
	other := errors.New("other")
	require.False(t, onlyMissingTenantSecrets(nil))
	require.True(t, onlyMissingTenantSecrets(fmt.Errorf("tenant: %w", errors.Join(missing, missing))))
	require.False(t, onlyMissingTenantSecrets(fmt.Errorf("tenant: %w", errors.Join(missing, other))))
}

func TestTenantKeyringAllowsSharedCurrentAndPreviousKeys(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	mapping := map[string]string{"alpha": "ns-a", "bravo": "ns-b"}
	changeTenantSource(t, ctx, c, "ns-a", "shared-old")
	changeTenantSource(t, ctx, c, "ns-b", "shared-old")
	ring, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	require.Len(t, ring.Data, 2, "shared key values must not merge tenant entries")
	for _, tenant := range []string{"alpha", "bravo"} {
		require.Equal(t, []byte(`["shared-old"]`), ring.Data[tenant])
	}
	changeTenantSource(t, ctx, c, "ns-a", "shared-new")
	ring, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	require.Equal(t, []byte(`["shared-new","shared-old"]`), ring.Data["alpha"])
	require.Equal(t, []byte(`["shared-old"]`), ring.Data["bravo"], "one tenant's previous key may be another tenant's current key")
	changeTenantSource(t, ctx, c, "ns-b", "shared-new")
	ring, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	for _, tenant := range []string{"alpha", "bravo"} {
		require.Equal(t, []byte(`["shared-new","shared-old"]`), ring.Data[tenant])
	}
	// Rotation limits are covered above; verify retirement is tenant-specific.
	changeTenantSource(t, ctx, c, "ns-a", "alpha-third")
	ring.Data["alpha"] = []byte(`["shared-new"]`)
	require.NoError(t, c.Update(ctx, ring))
	ring, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	require.Equal(t, []byte(`["alpha-third","shared-new"]`), ring.Data["alpha"])
	require.Equal(t, []byte(`["shared-new","shared-old"]`), ring.Data["bravo"], "retiring alpha's previous key must not retire bravo's key")
	// Removing a tenant removes its entry even if its key is still used by another tenant.
	ring, err = reconcileKVServiceTenantKeys(ctx, c, cr, map[string]string{"bravo": "ns-b"})
	require.NoError(t, err)
	require.NotContains(t, ring.Data, "alpha")
	require.Equal(t, []byte(`["shared-new","shared-old"]`), ring.Data["bravo"])
}

func TestTenantKeyringDiagnosticsDoNotExposeKeyMaterial(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	mapping := map[string]string{"alpha": "ns-a", "bravo": "ns-b"}
	ring, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	ring.Data["alpha"] = []byte(`{"private":"do-not-echo-this-key"`)
	require.NoError(t, c.Update(ctx, ring))
	_, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	var syntax *json.SyntaxError
	require.ErrorAs(t, err, &syntax)
	message, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	require.Contains(t, message, cr.Namespace+"/"+ring.Name)
	require.NotContains(t, err.Error(), "do-not-echo-this-key")
}
