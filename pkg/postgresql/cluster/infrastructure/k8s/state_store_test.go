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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// GetSourcePgVersion reads the running major straight from the live CNPG cluster
// (PGDataImageInfo.MajorVersion), not the provisioner's status.CurrentPgVersion
// projection — decoupling the major-upgrade use case from the provisioner.
func TestGetSourcePgVersionReadsLiveCNPGMajorVersion(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKey{Name: "pg1", Namespace: "default"}
	c := newCNPGTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Status: cnpgv1.ClusterStatus{
			PGDataImageInfo: &cnpgv1.ImageInfo{
				Image:        "ghcr.io/cloudnative-pg/postgresql:15.10",
				MajorVersion: 15,
			},
		},
	})

	version, err := NewClusterStateStore(c, key).GetSourcePgVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, "15", version)
}

// A not-yet-created CNPG cluster reports no source version (not an error): the
// upgrade simply has nothing to act on yet and Prerequisites defers it.
func TestGetSourcePgVersionReturnsEmptyWhenCNPGMissing(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKey{Name: "pg1", Namespace: "default"}
	c := newCNPGTestClient(t)

	version, err := NewClusterStateStore(c, key).GetSourcePgVersion(ctx)
	require.NoError(t, err)
	assert.Empty(t, version)
}

// A CNPG cluster that has not yet published PGDataImageInfo reports no source
// version rather than a spurious "0".
func TestGetSourcePgVersionReturnsEmptyWhenImageInfoUnset(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKey{Name: "pg1", Namespace: "default"}
	c := newCNPGTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
	})

	version, err := NewClusterStateStore(c, key).GetSourcePgVersion(ctx)
	require.NoError(t, err)
	assert.Empty(t, version)
}

func newCNPGTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}
