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

	dbclusterreadiness "github.com/splunk/splunk-operator/pkg/postgresql/database/core/components/clusterreadiness"
	dbk8s "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/k8s"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ dbclusterreadiness.ClusterReader = (*clusterReader)(nil)

type clusterReader struct {
	reader dbk8s.ClusterReader
}

// NewClusterReader returns the database adapter for Kubernetes PostgresCluster
// reads and provider-status translation.
func NewClusterReader(reader client.Reader) dbclusterreadiness.ClusterReader {
	return &clusterReader{reader: dbk8s.NewClusterReader(reader)}
}

func (r *clusterReader) Read(ctx context.Context, namespace, name string) (dbclusterreadiness.ResolvedClusterFacts, error) {
	snapshot, err := r.reader.Read(ctx, namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return dbclusterreadiness.ResolvedClusterFacts{}, fmt.Errorf("%w: %w", dbclusterreadiness.ErrClusterNotFound, err)
		}
		return dbclusterreadiness.ResolvedClusterFacts{}, err
	}

	facts := dbclusterreadiness.ResolvedClusterFacts{
		Name:                   snapshot.Name,
		Namespace:              snapshot.Namespace,
		Recovery:               dbclusterreadiness.RecoveryNone,
		ManagedRolesStatus:     snapshot.ManagedRolesStatus,
		ConnectionPoolerStatus: snapshot.ConnectionPoolerStatus,
		CustomMetricsStatus:    snapshot.CustomMetricsStatus,
	}
	if snapshot.Phase != nil {
		facts.Lifecycle = dbclusterreadiness.Lifecycle(*snapshot.Phase)
	}
	if snapshot.ProvisionerRef != nil {
		facts.Provider = &dbclusterreadiness.ProviderReference{
			Kind:      dbclusterreadiness.ProviderCNPG,
			Name:      snapshot.ProvisionerRef.Name,
			Namespace: snapshot.ProvisionerRef.Namespace,
		}
	}
	if snapshot.Resources != nil {
		facts.SuperUserSecretRef = snapshot.Resources.SuperUserSecretRef
	}
	if condition := meta.FindStatusCondition(snapshot.Conditions, pgcnpg.ClusterReadyCondition); condition != nil &&
		(condition.Reason == pgcnpg.ClusterReadyReasonRecovery || condition.Reason == pgcnpg.ClusterReadyReasonFailingOver) {
		facts.Recovery = dbclusterreadiness.RecoveryInProgress
	}
	return facts, nil
}
