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
			got.EndpointWithdrawalDelaySeconds != 30 ||
			got.DetentionTimeoutSeconds != 180 ||
			got.SearchDrainTimeoutSeconds != 180 ||
			got.CaptainTransferTimeoutSeconds != 180 ||
			got.PodStartupTimeoutSeconds != 1800 ||
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
				PodUpdateStrategy:              enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
				EndpointWithdrawalDelaySeconds: lifecycleInt64Pointer(100),
				DetentionTimeoutSeconds:        lifecycleInt64Pointer(105),
				SearchDrainTimeoutSeconds:      lifecycleInt64Pointer(101),
				CaptainTransferTimeoutSeconds:  lifecycleInt64Pointer(102),
				PodStartupTimeoutSeconds:       lifecycleInt64Pointer(103),
				MemberRejoinTimeoutSeconds:     lifecycleInt64Pointer(104),
			},
		}
		got, err := ResolveSearchHeadClusterLifecyclePolicy(spec)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got.TerminationGracePeriodSeconds != 100 ||
			got.PodUpdateStrategy != enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate ||
			got.EndpointWithdrawalDelaySeconds != 100 ||
			got.DetentionTimeoutSeconds != 105 ||
			got.SearchDrainTimeoutSeconds != 101 ||
			got.CaptainTransferTimeoutSeconds != 102 ||
			got.PodStartupTimeoutSeconds != 103 ||
			got.MemberRejoinTimeoutSeconds != 104 {
			t.Fatalf("unexpected explicit policy: %#v", got)
		}
	})
}

func TestValidateSearchHeadClusterLifecyclePolicy(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		value     int64
		detention *int64
		wantErr   bool
	}{
		{name: "minimum", value: 1},
		{name: "below default detention timeout", value: 179},
		{name: "equal to default detention timeout", value: 180, wantErr: true},
		{
			name:      "custom coherent timing",
			value:     60,
			detention: lifecycleInt64Pointer(61),
		},
		{
			name:      "custom incoherent timing",
			value:     60,
			detention: lifecycleInt64Pointer(60),
			wantErr:   true,
		},
		{name: "maximum is incompatible with maximum detention timeout", value: 86400, wantErr: true},
		{name: "zero", value: 0, wantErr: true},
		{name: "above maximum", value: 86401, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			spec := &enterpriseApi.SearchHeadClusterSpec{
				LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
					EndpointWithdrawalDelaySeconds: lifecycleInt64Pointer(testCase.value),
					DetentionTimeoutSeconds:        testCase.detention,
				},
			}
			err := validateSearchHeadClusterLifecyclePolicy(spec)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validation error = %v, wantErr=%v", err, testCase.wantErr)
			}
		})
	}

	defaultConflict := &enterpriseApi.SearchHeadClusterSpec{
		LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
			DetentionTimeoutSeconds: lifecycleInt64Pointer(30),
		},
	}
	if err := validateSearchHeadClusterLifecyclePolicy(defaultConflict); err == nil {
		t.Fatal("expected default endpoint-withdrawal delay to be validated against explicit detention timeout")
	}
}

func TestValidateSearchHeadClusterImageUpdateIntent(t *testing.T) {
	source := "registry.example/splunk@sha256:source"
	target := "registry.example/splunk@sha256:target"
	valid := func() *enterpriseApi.SearchHeadClusterSpec {
		return &enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: target},
			},
			LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
				ImageUpdateIntent: &enterpriseApi.SearchHeadClusterImageUpdateIntentSpec{
					Intent:      enterpriseApi.SearchHeadClusterImageUpdateIntentSameVersionRestart,
					SourceImage: source,
					TargetImage: target,
				},
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*enterpriseApi.SearchHeadClusterSpec)
		wantErr bool
	}{
		{name: "omitted"},
		{name: "exact pair", mutate: func(*enterpriseApi.SearchHeadClusterSpec) {}},
		{
			name: "requires RollingUpdate",
			mutate: func(spec *enterpriseApi.SearchHeadClusterSpec) {
				spec.LifecyclePolicy.PodUpdateStrategy = enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete
			},
			wantErr: true,
		},
		{
			name: "requires source",
			mutate: func(spec *enterpriseApi.SearchHeadClusterSpec) {
				spec.LifecyclePolicy.ImageUpdateIntent.SourceImage = ""
			},
			wantErr: true,
		},
		{
			name: "requires different images",
			mutate: func(spec *enterpriseApi.SearchHeadClusterSpec) {
				spec.LifecyclePolicy.ImageUpdateIntent.SourceImage = target
			},
			wantErr: true,
		},
		{
			name: "binds target to spec image",
			mutate: func(spec *enterpriseApi.SearchHeadClusterSpec) {
				spec.Image = "registry.example/splunk@sha256:other"
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var spec *enterpriseApi.SearchHeadClusterSpec
			if test.mutate != nil {
				spec = valid()
				test.mutate(spec)
			}
			err := validateSearchHeadClusterImageUpdateIntent(spec)
			if (err != nil) != test.wantErr {
				t.Fatalf("validation error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
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
