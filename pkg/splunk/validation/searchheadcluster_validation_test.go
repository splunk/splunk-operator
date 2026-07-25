/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package validation

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestValidateSearchHeadClusterLifecyclePolicy(t *testing.T) {
	validPolicy := func() *enterpriseApi.SearchHeadClusterLifecyclePolicy {
		return &enterpriseApi.SearchHeadClusterLifecyclePolicy{
			PodUpdateStrategy:             enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			SearchDrainTimeoutSeconds:     int64Pointer(10),
			CaptainTransferTimeoutSeconds: int64Pointer(20),
			MemberRejoinTimeoutSeconds:    int64Pointer(30),
		}
	}

	tests := []struct {
		name         string
		podGate      bool
		shcGate      bool
		policy       *enterpriseApi.SearchHeadClusterLifecyclePolicy
		wantErrField string
	}{
		{name: "omitted with gates disabled"},
		{
			name:         "policy rejected with gates disabled",
			policy:       validPolicy(),
			wantErrField: "spec.lifecyclePolicy",
		},
		{
			name:         "SHC gate requires pod gate",
			shcGate:      true,
			policy:       validPolicy(),
			wantErrField: "spec.lifecyclePolicy",
		},
		{
			name:    "valid distinct values",
			podGate: true,
			shcGate: true,
			policy:  validPolicy(),
		},
		{
			name:    "empty policy resolves defaults",
			podGate: true,
			shcGate: true,
			policy:  &enterpriseApi.SearchHeadClusterLifecyclePolicy{},
		},
		{
			name:    "unknown strategy",
			podGate: true,
			shcGate: true,
			policy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy: "ReplaceEverything",
			},
			wantErrField: "spec.lifecyclePolicy.podUpdateStrategy",
		},
		{
			name:    "invalid drain timeout",
			podGate: true,
			shcGate: true,
			policy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				SearchDrainTimeoutSeconds: int64Pointer(0),
			},
			wantErrField: "spec.lifecyclePolicy.searchDrainTimeoutSeconds",
		},
		{
			name:    "invalid captain timeout",
			podGate: true,
			shcGate: true,
			policy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				CaptainTransferTimeoutSeconds: int64Pointer(86401),
			},
			wantErrField: "spec.lifecyclePolicy.captainTransferTimeoutSeconds",
		},
		{
			name:    "invalid rejoin timeout",
			podGate: true,
			shcGate: true,
			policy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				MemberRejoinTimeoutSeconds: int64Pointer(-1),
			},
			wantErrField: "spec.lifecyclePolicy.memberRejoinTimeoutSeconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setLifecycleFeatureGatesForTest(t, tt.podGate, tt.shcGate)
			obj := &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas:        3,
					LifecyclePolicy: tt.policy,
				},
			}
			errs := ValidateSearchHeadClusterCreate(obj)
			if tt.wantErrField == "" {
				assert.Empty(t, errs)
				return
			}
			if assert.NotEmpty(t, errs) {
				assert.Equal(t, tt.wantErrField, errs[0].Field)
			}
		})
	}

	t.Run("termination grace and policy validate together", func(t *testing.T) {
		setLifecycleFeatureGatesForTest(t, true, true)
		obj := &enterpriseApi.SearchHeadCluster{
			Spec: enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					TerminationGracePeriodSeconds: int64Pointer(1200),
				},
				Replicas:        3,
				LifecyclePolicy: validPolicy(),
			},
		}
		assert.Empty(t, ValidateSearchHeadClusterCreate(obj))
	})

	t.Run("feature gate names remain stable", func(t *testing.T) {
		assert.Equal(t, "SplunkPodLifecycle", string(config.SplunkPodLifecycle))
		assert.Equal(t, "SearchHeadClusterLifecycle", string(config.SearchHeadClusterLifecycle))
	})
}

func TestValidateSearchHeadClusterCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.SearchHeadCluster
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid search head cluster - minimal",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid search head cluster - zero replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 0,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "invalid search head cluster - less than 3 replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 2,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "invalid search head cluster - negative replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: -1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "valid search head cluster - with AppFramework",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "appvol", Endpoint: "s3://apps"},
						},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "apps", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{VolName: "appvol"}},
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid search head cluster - AppFramework source without name",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList:  []enterpriseApi.VolumeSpec{{Name: "vol", Endpoint: "s3://bucket"}},
						Defaults: enterpriseApi.AppSourceDefaultSpec{VolName: "vol"},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "", Location: "/apps"},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appRepo.appSources[0].name",
		},
		{
			name: "invalid search head cluster - AppFramework source without location",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList:  []enterpriseApi.VolumeSpec{{Name: "vol", Endpoint: "s3://bucket"}},
						Defaults: enterpriseApi.AppSourceDefaultSpec{VolName: "vol"},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "apps", Location: ""},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appRepo.appSources[0].location",
		},
		{
			name: "invalid search head cluster - multiple errors",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: -1,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList:  []enterpriseApi.VolumeSpec{{Name: "vol", Endpoint: "s3://bucket"}},
						Defaults: enterpriseApi.AppSourceDefaultSpec{VolName: "vol"},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "", Location: ""},
						},
					},
				},
			},
			wantErrCount: 3, // negative replicas + missing name + missing location
		},
		// ES premium app ssl_enablement tests
		{
			name: "ES app with ssl_enablement strict is valid",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "s3://bucket"},
						},
						AppSources: []enterpriseApi.AppSourceSpec{
							{
								Name:     "es",
								Location: "/es",
								AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
									VolName: "vol1",
									Scope:   "premiumApps",
									PremiumAppsProps: enterpriseApi.PremiumAppsProps{
										Type:       "enterpriseSecurity",
										EsDefaults: enterpriseApi.EsDefaults{SslEnablement: "strict"},
									},
								},
							},
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "ES app with ssl_enablement ignore is valid",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "s3://bucket"},
						},
						AppSources: []enterpriseApi.AppSourceSpec{
							{
								Name:     "es",
								Location: "/es",
								AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
									VolName: "vol1",
									Scope:   "premiumApps",
									PremiumAppsProps: enterpriseApi.PremiumAppsProps{
										Type:       "enterpriseSecurity",
										EsDefaults: enterpriseApi.EsDefaults{SslEnablement: "ignore"},
									},
								},
							},
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "ES app with ssl_enablement auto is invalid on SHC",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "s3://bucket"},
						},
						AppSources: []enterpriseApi.AppSourceSpec{
							{
								Name:     "es",
								Location: "/es",
								AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
									VolName: "vol1",
									Scope:   "premiumApps",
									PremiumAppsProps: enterpriseApi.PremiumAppsProps{
										Type:       "enterpriseSecurity",
										EsDefaults: enterpriseApi.EsDefaults{SslEnablement: "auto"},
									},
								},
							},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appRepo.appSources[0].premiumAppsProps.esDefaults.sslEnablement",
		},
		{
			name: "ES app with auto ssl_enablement from defaults is invalid on SHC",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "s3://bucket"},
						},
						Defaults: enterpriseApi.AppSourceDefaultSpec{
							VolName: "vol1",
							Scope:   "premiumApps",
							PremiumAppsProps: enterpriseApi.PremiumAppsProps{
								Type:       "enterpriseSecurity",
								EsDefaults: enterpriseApi.EsDefaults{SslEnablement: "auto"},
							},
						},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "es", Location: "/es"},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appRepo.appSources[0].premiumAppsProps.esDefaults.sslEnablement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateSearchHeadClusterCreate(tt.obj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}

func TestValidateSearchHeadClusterUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.SearchHeadCluster
		oldObj       *enterpriseApi.SearchHeadCluster
		wantErrCount int
	}{
		{
			name: "valid update - same replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			oldObj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid update - scale up",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 5,
				},
			},
			oldObj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - negative replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: -1,
				},
			},
			oldObj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateSearchHeadClusterUpdate(tt.obj, tt.oldObj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

// TestValidateSearchHeadClusterInlineDefaultsRestartSafety qualifies the
// admission portion of OPS-008. Reconciliation independently repeats this
// classification; shc_secret rotation protection remains separate.
func TestValidateSearchHeadClusterInlineDefaultsRestartSafety(t *testing.T) {
	const (
		unsafeThree = `splunk:
  conf:
    server:
      content:
        shclustering:
          replication_factor: 3
`
		unsafeFive = `splunk:
  conf:
    server:
      content:
        shclustering:
          replication_factor: 5
`
		allowedOld = `splunk:
  conf:
    server:
      content:
        shclustering:
          captain_is_adhoc_searchhead: false
          shcluster_label: old
`
		allowedNew = `splunk:
  conf:
    server:
      content:
        shclustering:
          captain_is_adhoc_searchhead: true
          shcluster_label: new
`
	)

	newSHC := func(defaults string, strategy enterpriseApi.SearchHeadClusterPodUpdateStrategy) *enterpriseApi.SearchHeadCluster {
		obj := &enterpriseApi.SearchHeadCluster{
			Spec: enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Defaults: defaults,
				},
				Replicas: 3,
			},
		}
		if strategy != "" {
			obj.Spec.LifecyclePolicy = &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy: strategy,
			}
		}
		return obj
	}

	tests := []struct {
		name        string
		oldDefaults string
		defaults    string
		strategy    enterpriseApi.SearchHeadClusterPodUpdateStrategy
		wantError   bool
	}{
		{
			name:        "unchanged unsafe stanza is allowed",
			oldDefaults: unsafeThree,
			defaults:    unsafeThree,
		},
		{
			name:        "changing an unsafe setting is rejected",
			oldDefaults: unsafeThree,
			defaults:    unsafeFive,
			wantError:   true,
		},
		{
			name:      "adding an unsafe setting is rejected",
			defaults:  unsafeThree,
			wantError: true,
		},
		{
			name:        "removing an unsafe setting is rejected",
			oldDefaults: unsafeThree,
			wantError:   true,
		},
		{
			name:        "documented rolling compatible settings are allowed",
			oldDefaults: allowedOld,
			defaults:    allowedNew,
		},
		{
			name: "adding only captain setting is allowed",
			defaults: `splunk:
  conf:
    server:
      content:
        shclustering:
          captain_is_adhoc_searchhead: true
`,
		},
		{
			name: "adding only label is allowed",
			defaults: `splunk:
  conf:
    server:
      content:
        shclustering:
          shcluster_label: production
`,
		},
		{
			name:        "unrelated defaults change with unchanged unsafe stanza is allowed",
			oldDefaults: unsafeThree + "  http_enableSSL: false\n",
			defaults:    unsafeThree + "  http_enableSSL: true\n",
		},
		{
			name:        "malformed changed defaults fail closed",
			oldDefaults: unsafeThree,
			defaults:    "splunk: [",
			wantError:   true,
		},
		{
			name:        "unsupported shclustering value fails closed",
			oldDefaults: unsafeThree,
			defaults: `splunk:
  conf:
    server:
      content:
        shclustering:
          - replication_factor: 5
`,
			wantError: true,
		},
		{
			name:        "OnDelete cannot admit simultaneous restart setting",
			oldDefaults: unsafeThree,
			defaults:    unsafeFive,
			strategy:    enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete,
			wantError:   true,
		},
		{
			name:        "RollingUpdate cannot admit simultaneous restart setting",
			oldDefaults: unsafeThree,
			defaults:    unsafeFive,
			strategy:    enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			wantError:   true,
		},
		{
			name: "key value sequence form is classified",
			oldDefaults: `splunk:
  conf:
    - key: server
      value:
        content:
          shclustering:
            captain_is_adhoc_searchhead: false
`,
			defaults: `splunk:
  conf:
    - key: server
      value:
        content:
          shclustering:
            captain_is_adhoc_searchhead: true
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setLifecycleFeatureGatesForTest(t, true, true)
			obj := newSHC(tt.defaults, tt.strategy)
			oldObj := newSHC(tt.oldDefaults, tt.strategy)

			errs := ValidateSearchHeadClusterUpdate(obj, oldObj)
			if tt.wantError {
				if assert.NotEmpty(t, errs) {
					assert.Equal(t, "spec.defaults", errs[len(errs)-1].Field)
				}
				return
			}
			assert.Empty(t, errs)
		})
	}

	t.Run("create with unsafe inline setting remains allowed", func(t *testing.T) {
		setLifecycleFeatureGatesForTest(t, true, true)
		assert.Empty(t, ValidateSearchHeadClusterCreate(newSHC(
			unsafeThree,
			enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
		)))
	})
}

func TestGetSearchHeadClusterWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}
	warnings := GetSearchHeadClusterWarningsOnCreate(obj)
	assert.Empty(t, warnings, "expected no warnings")
}

func TestGetSearchHeadClusterWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}
	oldObj := &enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}
	warnings := GetSearchHeadClusterWarningsOnUpdate(obj, oldObj)
	assert.Empty(t, warnings, "expected no warnings")
}
