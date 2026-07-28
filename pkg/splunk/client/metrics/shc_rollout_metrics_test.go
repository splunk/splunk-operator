// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSHCRolloutDecisionMetricHasOnlyBoundedLabels(t *testing.T) {
	descriptions := make(chan *prometheus.Desc, 1)
	SHCRolloutDecisionCounters.Describe(descriptions)
	description := (<-descriptions).String()

	if !strings.Contains(description, "variableLabels: {action,reason}") {
		t.Fatalf("metric description = %q, want action and reason labels", description)
	}
	for _, forbidden := range []string{
		"namespace",
		"name",
		"operation",
		"revision",
		"pod",
		"uid",
		"message",
	} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("metric description contains unbounded label %q: %s",
				forbidden, description)
		}
	}
}

func TestSHCSearchDrainContinuationApprovalMetricHasNoLabels(t *testing.T) {
	descriptions := make(chan *prometheus.Desc, 1)
	SHCSearchDrainContinuationApprovalCounter.Describe(descriptions)
	description := (<-descriptions).String()

	if !strings.Contains(
		description,
		"fqName: \"splunk_operator_shc_search_drain_continuation_approval_total\"",
	) {
		t.Fatalf("metric description = %q, want continuation approval counter", description)
	}
	if !strings.Contains(description, "variableLabels: {}") {
		t.Fatalf("approval metric must not contain variable labels: %s", description)
	}
}
