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
	dbk8s "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/k8s"
	dbclusterinfo "github.com/splunk/splunk-operator/pkg/postgresql/database/ports/clusterinfo"
	dbidentity "github.com/splunk/splunk-operator/pkg/postgresql/database/ports/identity"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ dbclusterinfo.ClusterReader = (*clusterReader)(nil)

type clusterReader struct {
	reader   dbk8s.ClusterReader
	resolver dbidentity.ClusterCardResolver
}

// NewClusterReader returns the database adapter for Kubernetes PostgresCluster
// reads and provider-status translation. Production callers provide the shared
// resolver constructed by the composition root. A nil resolver is accepted for
// focused adapter tests and uses the same stateless implementation.
func NewClusterReader(reader client.Reader, resolvers ...dbidentity.ClusterCardResolver) *clusterReader {
	resolver := dbidentity.ClusterCardResolver(identityadapter.NewIdentityResolver())
	if len(resolvers) > 0 && resolvers[0] != nil {
		resolver = resolvers[0]
	}
	return &clusterReader{reader: dbk8s.NewClusterReader(reader), resolver: resolver}
}

func (r *clusterReader) Read(ctx context.Context, namespace, name string) (dbclusterinfo.ResolvedClusterFacts, error) {
	snapshot, err := r.reader.Read(ctx, namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return dbclusterinfo.ResolvedClusterFacts{}, fmt.Errorf("%w: %w", dbclusterinfo.ErrClusterNotFound, err)
		}
		return dbclusterinfo.ResolvedClusterFacts{}, err
	}

	facts := dbclusterinfo.ResolvedClusterFacts{
		Name:                   snapshot.Name,
		Namespace:              snapshot.Namespace,
		Recovery:               dbclusterinfo.RecoveryNone,
		ManagedRolesStatus:     snapshot.ManagedRolesStatus,
		ConnectionPoolerStatus: snapshot.ConnectionPoolerStatus,
		CustomMetricsStatus:    snapshot.CustomMetricsStatus,
	}
	if snapshot.Phase != nil {
		facts.Lifecycle = dbclusterinfo.Lifecycle(*snapshot.Phase)
	}
	card, err := r.resolveClusterCard(snapshot)
	if err != nil {
		return dbclusterinfo.ResolvedClusterFacts{}, err
	}
	facts.Cluster = &card
	if snapshot.Resources != nil {
		facts.SuperUserSecretRef = snapshot.Resources.SuperUserSecretRef
	}
	if condition := meta.FindStatusCondition(snapshot.Conditions, pgcnpg.ClusterReadyCondition); condition != nil &&
		(condition.Reason == pgcnpg.ClusterReadyReasonRecovery || condition.Reason == pgcnpg.ClusterReadyReasonFailingOver) {
		facts.Recovery = dbclusterinfo.RecoveryInProgress
	}
	return facts, nil
}

func (r *clusterReader) resolveClusterCard(snapshot dbk8s.ClusterSnapshot) (identitytypes.ClusterCard, error) {
	input, err := identityadapter.ClusterInputFromPostgresCluster(&platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: snapshot.Name, Namespace: snapshot.Namespace, UID: snapshot.UID},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef:             snapshot.ProvisionerRef,
			PostgresMajorUpgradeStatus: snapshot.MajorUpgrades,
		},
	})
	if err != nil {
		return identitytypes.ClusterCard{}, fmt.Errorf("resolving PostgresCluster identity input: %w", err)
	}
	card, err := r.resolver.ResolveCluster(input)
	if err != nil {
		return identitytypes.ClusterCard{}, fmt.Errorf("resolving PostgresCluster authority: %w", err)
	}
	return card, nil
}
