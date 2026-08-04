// Copyright (c) 2026 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestIndexerEndpointWithdrawalDelay(t *testing.T) {
	require.Equal(
		t,
		30*time.Second,
		indexerEndpointWithdrawalDelay(&enterpriseApi.IndexerClusterSpec{}),
	)
	delaySeconds := int64(45)
	require.Equal(
		t,
		45*time.Second,
		indexerEndpointWithdrawalDelay(&enterpriseApi.IndexerClusterSpec{
			LifecyclePolicy: &enterpriseApi.IndexerClusterLifecyclePolicy{
				EndpointWithdrawalDelaySeconds: &delaySeconds,
			},
		}),
	)
}

func TestValidateIndexerEndpointWithdrawalDelay(t *testing.T) {
	for _, delaySeconds := range []int64{0, -1, 86401} {
		cr := &enterpriseApi.IndexerCluster{
			Spec: enterpriseApi.IndexerClusterSpec{
				Replicas: 1,
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					ClusterManagerRef: corev1.ObjectReference{Name: "manager"},
				},
				LifecyclePolicy: &enterpriseApi.IndexerClusterLifecyclePolicy{
					EndpointWithdrawalDelaySeconds: &delaySeconds,
				},
			},
		}
		err := validateIndexerClusterSpec(
			context.Background(),
			spltest.NewMockClient(),
			cr,
		)
		require.ErrorContains(
			t,
			err,
			"lifecyclePolicy.endpointWithdrawalDelaySeconds",
		)
	}
}
