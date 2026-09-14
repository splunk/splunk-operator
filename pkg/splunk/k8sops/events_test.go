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

package k8sops

import (
	"context"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestK8EventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(2)
	publisher, err := NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.Standalone{})
	require.NoError(t, err)

	ctx := context.Background()
	publisher.Normal(ctx, "NormalReason", "normal message")
	publisher.Warning(ctx, "WarningReason", "warning message")

	assert.Eventually(t, func() bool {
		return len(recorder.Events) == 2
	}, time.Second, time.Millisecond)
	assert.Contains(t, <-recorder.Events, "NormalReason")
	assert.Contains(t, <-recorder.Events, "WarningReason")
}

func TestEmitStalledTransitionEvents(t *testing.T) {
	recorder := record.NewFakeRecorder(2)
	publisher, err := NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.Standalone{})
	require.NoError(t, err)

	stalled := []metav1.Condition{{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionTrue}}
	resolved := []metav1.Condition{{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionFalse}}

	EmitStalledTransitionEvents(context.Background(), publisher, "standalone", nil, stalled)
	EmitStalledTransitionEvents(context.Background(), publisher, "standalone", stalled, resolved)

	assert.Eventually(t, func() bool {
		return len(recorder.Events) == 2
	}, time.Second, time.Millisecond)
	assert.Contains(t, <-recorder.Events, "Stalled")
	assert.Contains(t, <-recorder.Events, "StalledResolved")
}
