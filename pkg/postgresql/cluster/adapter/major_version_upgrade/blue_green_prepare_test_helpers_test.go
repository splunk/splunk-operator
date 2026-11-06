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

package majorupgradeadapter

import (
	"context"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
)

type candidateSourceVerifiedStub struct{}

func (candidateSourceVerifiedStub) VerifySourceBaseline(context.Context, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	return nil
}

func (candidateSourceVerifiedStub) VerifyAndGuard(context.Context, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	return nil
}

func (candidateSourceVerifiedStub) VerifyReplication(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return false, nil
}

type successfulSourcePreflight struct{}

type failingSourcePreflight struct{ successfulSourcePreflight }

func (failingSourcePreflight) Inspect(context.Context, *cnpgv1.Cluster, []declaredDatabase, string) ([]databasePreflight, error) {
	return nil, mvutypes.ErrUpgradeFlowFailed
}

func (successfulSourcePreflight) Inspect(_ context.Context, _ *cnpgv1.Cluster, databases []declaredDatabase, _ string) ([]databasePreflight, error) {
	result := make([]databasePreflight, 0, len(databases))
	for _, database := range databases {
		result = append(result, databasePreflight{name: database.name, schemaFingerprint: "schema-" + database.name, accessFingerprint: "access-" + database.name})
	}
	return result, nil
}

func (successfulSourcePreflight) EnsureReplicationRole(context.Context, *cnpgv1.Cluster, []declaredDatabase, string, string) error {
	return nil
}
