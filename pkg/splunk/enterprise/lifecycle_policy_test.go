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

package enterprise

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
)

func lifecycleInt64Pointer(value int64) *int64 {
	return &value
}

func setLifecyclePolicyTestGates(t *testing.T, podLifecycle, shcLifecycle bool) {
	t.Helper()
	oldPodLifecycle := config.DefaultMutableFeatureGate.Enabled(config.SplunkPodLifecycle)
	oldSHCLifecycle := config.DefaultMutableFeatureGate.Enabled(config.SearchHeadClusterLifecycle)
	if err := config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{
		string(config.SplunkPodLifecycle):         podLifecycle,
		string(config.SearchHeadClusterLifecycle): shcLifecycle,
	}); err != nil {
		t.Fatalf("set lifecycle feature gates: %v", err)
	}
	t.Cleanup(func() {
		if err := config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{
			string(config.SplunkPodLifecycle):         oldPodLifecycle,
			string(config.SearchHeadClusterLifecycle): oldSHCLifecycle,
		}); err != nil {
			t.Errorf("restore lifecycle feature gates: %v", err)
		}
	})
}

func TestResolveSearchHeadClusterLifecyclePolicy(t *testing.T) {
	t.Run("nil spec is rejected", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, true)
		if _, err := ResolveSearchHeadClusterLifecyclePolicy(nil); err == nil {
			t.Fatal("expected nil spec error")
		}
	})

	t.Run("disabled gate does not resolve defaults", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, false, false)
		if _, err := ResolveSearchHeadClusterLifecyclePolicy(&enterpriseApi.SearchHeadClusterSpec{}); err == nil {
			t.Fatal("expected disabled gate error")
		}
	})

	t.Run("dependency is enforced", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, false, true)
		if _, err := ResolveSearchHeadClusterLifecyclePolicy(&enterpriseApi.SearchHeadClusterSpec{}); err == nil {
			t.Fatal("expected feature dependency error")
		}
	})

	t.Run("omitted fields resolve spike defaults", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, true)
		got, err := ResolveSearchHeadClusterLifecyclePolicy(&enterpriseApi.SearchHeadClusterSpec{})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got.TerminationGracePeriodSeconds != 1200 ||
			got.PodUpdateStrategy != enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete ||
			got.SearchDrainTimeoutSeconds != 180 ||
			got.CaptainTransferTimeoutSeconds != 180 ||
			got.MemberRejoinTimeoutSeconds != 1800 {
			t.Fatalf("unexpected defaults: %#v", got)
		}
	})

	t.Run("explicit fields remain independent", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, true)
		spec := &enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				TerminationGracePeriodSeconds: lifecycleInt64Pointer(100),
			},
			LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy:             enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
				SearchDrainTimeoutSeconds:     lifecycleInt64Pointer(101),
				CaptainTransferTimeoutSeconds: lifecycleInt64Pointer(102),
				MemberRejoinTimeoutSeconds:    lifecycleInt64Pointer(103),
			},
		}
		got, err := ResolveSearchHeadClusterLifecyclePolicy(spec)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got.TerminationGracePeriodSeconds != 100 ||
			got.PodUpdateStrategy != enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate ||
			got.SearchDrainTimeoutSeconds != 101 ||
			got.CaptainTransferTimeoutSeconds != 102 ||
			got.MemberRejoinTimeoutSeconds != 103 {
			t.Fatalf("unexpected explicit policy: %#v", got)
		}
	})
}

func TestResolveTerminationGracePeriodSeconds(t *testing.T) {
	t.Run("nil spec does not panic", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, false)
		if got := ResolveTerminationGracePeriodSeconds(nil); got != nil {
			t.Fatalf("nil spec resolved a value: %d", *got)
		}
	})

	t.Run("disabled preserves existing pod template", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, false, false)
		spec := &enterpriseApi.CommonSplunkSpec{
			TerminationGracePeriodSeconds: lifecycleInt64Pointer(100),
		}
		if got := ResolveTerminationGracePeriodSeconds(spec); got != nil {
			t.Fatalf("disabled gate resolved a value: %d", *got)
		}
	})

	t.Run("enabled resolves default", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, false)
		got := ResolveTerminationGracePeriodSeconds(&enterpriseApi.CommonSplunkSpec{})
		if got == nil || *got != DefaultTerminationGracePeriodSeconds {
			t.Fatalf("got %v, want %d", got, DefaultTerminationGracePeriodSeconds)
		}
	})

	t.Run("enabled preserves explicit value", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, false)
		spec := &enterpriseApi.CommonSplunkSpec{
			TerminationGracePeriodSeconds: lifecycleInt64Pointer(100),
		}
		got := ResolveTerminationGracePeriodSeconds(spec)
		if got == nil || *got != 100 {
			t.Fatalf("got %v, want 100", got)
		}
	})
}
