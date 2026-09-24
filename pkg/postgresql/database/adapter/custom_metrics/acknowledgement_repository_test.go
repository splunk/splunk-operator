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

package custom_metrics

import (
	"testing"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAcknowledgementRepositoryMatchesCompleteIdentity(t *testing.T) {
	identity := mtypes.ContributorIdentity{
		PostgresDatabaseName: "owner",
		PostgresDatabaseUID:  "uid",
		DatabaseName:         "orders",
	}
	repository := NewAcknowledgementRepository(&enterprisev4.CustomMetricsStatus{
		DatabaseContributions: []enterprisev4.DatabaseCustomMetricsStatus{{
			PostgresDatabaseName: "owner",
			PostgresDatabaseUID:  "uid",
			DatabaseName:         "orders",
			DesiredRevision:      "desired",
			AppliedRevision:      "applied",
			Status:               metav1.ConditionFalse,
			Reason:               "InvalidQueryDefinition",
			Message:              "invalid source",
		}},
	})

	ack, found, err := repository.Find(t.Context(), identity)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, mtypes.AcknowledgementFalse, ack.Status)
	assert.Equal(t, "desired", ack.DesiredRevision)
	assert.Equal(t, "applied", ack.AppliedRevision)

	identity.PostgresDatabaseUID = "recreated-owner"
	_, found, err = repository.Find(t.Context(), identity)
	require.NoError(t, err)
	assert.False(t, found, "an acknowledgement for a deleted object must not match its replacement")
}
