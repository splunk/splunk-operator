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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// stubComponent is a minimal component implementation for wiring tests.
type stubComponent struct {
	name     string
	requires []contractKey
	provides []contractKey
}

func (s *stubComponent) Name() string                      { return s.name }
func (s *stubComponent) Requires() []contractKey           { return s.requires }
func (s *stubComponent) Provides() []contractKey           { return s.provides }
func (s *stubComponent) CheckContracts() error             { return nil }
func (s *stubComponent) Reconcile(_ context.Context) error { return nil }
func (s *stubComponent) Observe(_ context.Context, _ error) (componentHealth, error) {
	return componentHealth{}, nil
}

func stub(name string, requires, provides []contractKey) *stubComponent {
	return &stubComponent{name: name, requires: requires, provides: provides}
}

func TestValidateComponentOrder(t *testing.T) {
	t.Parallel()

	t.Run("valid order passes", func(t *testing.T) {
		t.Parallel()
		components := []component{
			stub("secret", nil, []contractKey{contractSecret}),
			stub("cluster", []contractKey{contractSecret}, []contractKey{contractCNPGCluster}),
			stub("roles", []contractKey{contractCNPGCluster, contractSecret}, nil),
		}
		assert.NoError(t, validateComponentOrder(components))
	})

	t.Run("no dependencies passes", func(t *testing.T) {
		t.Parallel()
		components := []component{
			stub("a", nil, nil),
			stub("b", nil, nil),
		}
		assert.NoError(t, validateComponentOrder(components))
	})

	t.Run("requires before provides fails", func(t *testing.T) {
		t.Parallel()
		components := []component{
			stub("cluster", []contractKey{contractSecret}, []contractKey{contractCNPGCluster}),
			stub("secret", nil, []contractKey{contractSecret}),
		}
		err := validateComponentOrder(components)
		require.Error(t, err)
		assert.Contains(t, err.Error(), string(contractSecret))
		assert.Contains(t, err.Error(), "cluster")
	})

	t.Run("provider missing entirely fails", func(t *testing.T) {
		t.Parallel()
		components := []component{
			stub("roles", []contractKey{contractCNPGCluster}, nil),
		}
		err := validateComponentOrder(components)
		require.Error(t, err)
		assert.Contains(t, err.Error(), string(contractCNPGCluster))
	})

	t.Run("canonical component order is valid", func(t *testing.T) {
		t.Parallel()
		scheme := newTestScheme()
		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		}
		clusterClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "cls"},
		}
		mergedConfig := &MergedConfig{Spec: &enterprisev4.PostgresClusterSpec{}, CNPG: &enterprisev4.CNPGConfig{}}
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		contracts := &reconcileContracts{}
		components := []component{
			newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg1-secret", contracts),
			newClusterModel(c, scheme, noopEventEmitter{}, nil, cluster, clusterClass, mergedConfig, contracts),
			newManagedRolesModel(c, scheme, noopEventEmitter{}, nil, cluster, contracts),
			newPoolerModel(c, scheme, noopEventEmitter{}, nil, cluster, clusterClass, mergedConfig, contracts),
			newBackupModel(c, scheme, noopEventEmitter{}, nil, cluster, mergedConfig, contracts),
			newConfigMapModel(c, scheme, noopEventEmitter{}, nil, cluster, contracts),
		}
		assert.NoError(t, validateComponentOrder(components))
	})
}

func TestCheckContractsFromRequirements(t *testing.T) {
	t.Parallel()

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}}
	cnpg := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1"}}

	tests := []struct {
		name      string
		requires  []contractKey
		contracts *reconcileContracts
		want      bool
	}{
		{
			name:      "no requirements always satisfied",
			requires:  nil,
			contracts: &reconcileContracts{},
			want:      true,
		},
		{
			name:      "Secret required and present",
			requires:  []contractKey{contractSecret},
			contracts: &reconcileContracts{Secret: secret},
			want:      true,
		},
		{
			name:      "Secret required but nil",
			requires:  []contractKey{contractSecret},
			contracts: &reconcileContracts{},
			want:      false,
		},
		{
			name:      "CNPGCluster required and present",
			requires:  []contractKey{contractCNPGCluster},
			contracts: &reconcileContracts{CNPGCluster: cnpg},
			want:      true,
		},
		{
			name:      "CNPGCluster required but nil",
			requires:  []contractKey{contractCNPGCluster},
			contracts: &reconcileContracts{},
			want:      false,
		},
		{
			name:      "both required and both present",
			requires:  []contractKey{contractCNPGCluster, contractSecret},
			contracts: &reconcileContracts{CNPGCluster: cnpg, Secret: secret},
			want:      true,
		},
		{
			name:      "both required but only Secret present",
			requires:  []contractKey{contractCNPGCluster, contractSecret},
			contracts: &reconcileContracts{Secret: secret},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, checkContractsFromRequirements(tt.requires, tt.contracts))
		})
	}
}
