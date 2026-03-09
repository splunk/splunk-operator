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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

func TestValidateCommonSplunkSpec(t *testing.T) {
	// Note: The following fields are validated via kubebuilder annotations, not webhook:
	// - ImagePullPolicy: +kubebuilder:validation:Enum
	// - LivenessInitialDelaySeconds: +kubebuilder:validation:Minimum=0
	// - ReadinessInitialDelaySeconds: +kubebuilder:validation:Minimum=0
	tests := []struct {
		name         string
		spec         *enterpriseApi.CommonSplunkSpec
		wantErrCount int
		wantErrField string
	}{
		{
			name:         "valid spec - empty",
			spec:         &enterpriseApi.CommonSplunkSpec{},
			wantErrCount: 0,
		},
		// extraEnv uniqueness validation tests
		{
			name: "extraEnv - unique names are valid",
			spec: &enterpriseApi.CommonSplunkSpec{
				ExtraEnv: []corev1.EnvVar{
					{Name: "VAR1", Value: "value1"},
					{Name: "VAR2", Value: "value2"},
					{Name: "VAR3", Value: "value3"},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "extraEnv - duplicate names are invalid",
			spec: &enterpriseApi.CommonSplunkSpec{
				ExtraEnv: []corev1.EnvVar{
					{Name: "VAR1", Value: "value1"},
					{Name: "VAR1", Value: "value2"},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.extraEnv[1].name",
		},
		{
			name: "extraEnv - multiple duplicates",
			spec: &enterpriseApi.CommonSplunkSpec{
				ExtraEnv: []corev1.EnvVar{
					{Name: "VAR1", Value: "value1"},
					{Name: "VAR1", Value: "value2"},
					{Name: "VAR1", Value: "value3"},
				},
			},
			wantErrCount: 2, // VAR1[1] and VAR1[2] are duplicates of VAR1[0]
		},
		// imagePullSecrets uniqueness validation tests
		{
			name: "imagePullSecrets - unique names are valid",
			spec: &enterpriseApi.CommonSplunkSpec{
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "secret1"},
					{Name: "secret2"},
					{Name: "secret3"},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "imagePullSecrets - duplicate names are invalid",
			spec: &enterpriseApi.CommonSplunkSpec{
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "my-secret"},
					{Name: "my-secret"},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.imagePullSecrets[1].name",
		},
		{
			name: "imagePullSecrets - multiple duplicates",
			spec: &enterpriseApi.CommonSplunkSpec{
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "my-secret"},
					{Name: "my-secret"},
					{Name: "my-secret"},
				},
			},
			wantErrCount: 2, // my-secret[1] and my-secret[2] are duplicates of my-secret[0]
		},
		// Probe validation tests
		{
			name: "livenessProbe - valid values",
			spec: &enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 30,
					TimeoutSeconds:      30,
					PeriodSeconds:       30,
					FailureThreshold:    3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "livenessProbe - initialDelaySeconds can be 0",
			spec: &enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 0,
					TimeoutSeconds:      1,
					PeriodSeconds:       1,
					FailureThreshold:    1,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "livenessProbe - negative initialDelaySeconds is invalid",
			spec: &enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: -1,
					TimeoutSeconds:      1,
					PeriodSeconds:       1,
					FailureThreshold:    1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.livenessProbe.initialDelaySeconds",
		},
		{
			name: "livenessProbe - timeoutSeconds 0 is invalid",
			spec: &enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 0,
					TimeoutSeconds:      0,
					PeriodSeconds:       1,
					FailureThreshold:    1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.livenessProbe.timeoutSeconds",
		},
		{
			name: "livenessProbe - periodSeconds 0 is invalid",
			spec: &enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 0,
					TimeoutSeconds:      1,
					PeriodSeconds:       0,
					FailureThreshold:    1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.livenessProbe.periodSeconds",
		},
		{
			name: "livenessProbe - failureThreshold 0 is invalid",
			spec: &enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 0,
					TimeoutSeconds:      1,
					PeriodSeconds:       1,
					FailureThreshold:    0,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.livenessProbe.failureThreshold",
		},
		{
			name: "livenessProbe - all invalid values",
			spec: &enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: -1,
					TimeoutSeconds:      0,
					PeriodSeconds:       0,
					FailureThreshold:    0,
				},
			},
			wantErrCount: 4,
		},
		{
			name: "readinessProbe - valid values",
			spec: &enterpriseApi.CommonSplunkSpec{
				ReadinessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 10,
					TimeoutSeconds:      5,
					PeriodSeconds:       5,
					FailureThreshold:    3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "startupProbe - valid values",
			spec: &enterpriseApi.CommonSplunkSpec{
				StartupProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 40,
					TimeoutSeconds:      30,
					PeriodSeconds:       30,
					FailureThreshold:    12,
				},
			},
			wantErrCount: 0,
		},
		// Resource requirements validation tests
		{
			name: "resources - valid: request equals limit",
			spec: &enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
							corev1.ResourceCPU:    resource.MustParse("500m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
							corev1.ResourceCPU:    resource.MustParse("500m"),
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "resources - valid: request less than limit",
			spec: &enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
							corev1.ResourceCPU:    resource.MustParse("250m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
							corev1.ResourceCPU:    resource.MustParse("500m"),
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "resources - invalid: memory request exceeds limit",
			spec: &enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.resources.requests.memory",
		},
		{
			name: "resources - invalid: cpu request exceeds limit",
			spec: &enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.resources.requests.cpu",
		},
		{
			name: "resources - invalid: both memory and cpu requests exceed limits",
			spec: &enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
							corev1.ResourceCPU:    resource.MustParse("500m"),
						},
					},
				},
			},
			wantErrCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateCommonSplunkSpec(tt.spec, field.NewPath("spec"))

			if len(errs) != tt.wantErrCount {
				t.Errorf("validateCommonSplunkSpec() got %d errors, want %d", len(errs), tt.wantErrCount)
				for _, e := range errs {
					t.Logf("  error: %s", e.Error())
				}
			}

			if tt.wantErrField != "" && len(errs) > 0 {
				found := false
				for _, e := range errs {
					if e.Field == tt.wantErrField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("validateCommonSplunkSpec() expected error on field %s", tt.wantErrField)
				}
			}
		})
	}
}

func TestValidateSmartStore(t *testing.T) {
	tests := []struct {
		name         string
		smartStore   *enterpriseApi.SmartStoreSpec
		wantErrCount int
	}{
		{
			name:         "empty smart store",
			smartStore:   &enterpriseApi.SmartStoreSpec{},
			wantErrCount: 0,
		},
		{
			name: "valid smart store with volumes and indexes",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "vol1", Endpoint: "s3://bucket"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{
						Name: "idx1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "vol1",
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "volume without name",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "", Endpoint: "s3://bucket"},
				},
			},
			wantErrCount: 1,
		},
		{
			name: "volume without endpoint or path",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "vol1", Endpoint: "", Path: ""},
				},
			},
			wantErrCount: 1,
		},
		{
			name: "index without name - with volume defined",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "vol1", Endpoint: "s3://bucket"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{
						Name: "",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "vol1",
						},
					},
				},
			},
			wantErrCount: 1, // missing index name
		},
		{
			name: "index without volume name and no defaults",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "vol1", Endpoint: "s3://bucket"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{
						Name: "idx1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "",
						},
					},
				},
			},
			wantErrCount: 1, // missing volumeName (no defaults either)
		},
		{
			name: "index with volumeName from defaults",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "vol1", Endpoint: "s3://bucket"},
				},
				Defaults: enterpriseApi.IndexConfDefaultsSpec{
					IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "vol1",
					},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{
						Name: "idx1",
					},
				},
			},
			wantErrCount: 0, // volumeName comes from defaults
		},
		{
			name: "index references non-existent volume",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "vol1", Endpoint: "s3://bucket"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{
						Name: "idx1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "nonexistent",
						},
					},
				},
			},
			wantErrCount: 1, // volumeName doesn't reference existing volume
		},
		{
			name: "indexes defined without any volumes",
			smartStore: &enterpriseApi.SmartStoreSpec{
				IndexList: []enterpriseApi.IndexSpec{
					{
						Name: "idx1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "vol1",
						},
					},
				},
			},
			wantErrCount: 2, // no volumes + volumeName doesn't reference existing volume
		},
		{
			name: "multiple validation errors",
			smartStore: &enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "", Endpoint: ""},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{
						Name: "",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "",
						},
					},
				},
			},
			wantErrCount: 4, // vol name required, vol endpoint/path required, idx name required, idx volumeName required
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateSmartStore(tt.smartStore, field.NewPath("spec").Child("smartstore"))

			if len(errs) != tt.wantErrCount {
				t.Errorf("validateSmartStore() got %d errors, want %d", len(errs), tt.wantErrCount)
				for _, e := range errs {
					t.Logf("  error: %s", e.Error())
				}
			}
		})
	}
}

func TestValidateAppFramework(t *testing.T) {
	tests := []struct {
		name         string
		appConfig    *enterpriseApi.AppFrameworkSpec
		wantErrCount int
		wantErrField string
	}{
		{
			name:         "empty app framework",
			appConfig:    &enterpriseApi.AppFrameworkSpec{},
			wantErrCount: 0,
		},
		{
			name: "valid app framework",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "vol1", Endpoint: "s3://bucket"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "source1", Location: "/apps"},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "app source without name",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "", Location: "/apps"},
				},
			},
			wantErrCount: 1,
		},
		{
			name: "app source without location",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "source1", Location: ""},
				},
			},
			wantErrCount: 1,
		},
		{
			name: "volume without name",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "", Endpoint: "s3://bucket"},
				},
			},
			wantErrCount: 1,
		},
		// appsRepoPollInterval validation tests
		{
			name: "appsRepoPollInterval - 0 is valid (disabled)",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 0,
			},
			wantErrCount: 0,
		},
		{
			name: "appsRepoPollInterval - 60 is valid (minimum)",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 60,
			},
			wantErrCount: 0,
		},
		{
			name: "appsRepoPollInterval - 3600 is valid (1 hour)",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 3600,
			},
			wantErrCount: 0,
		},
		{
			name: "appsRepoPollInterval - 86400 is valid (maximum, 1 day)",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 86400,
			},
			wantErrCount: 0,
		},
		{
			name: "appsRepoPollInterval - negative value is invalid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: -1,
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appsRepoPollIntervalSeconds",
		},
		{
			name: "appsRepoPollInterval - 1 is invalid (between 0 and 60)",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 1,
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appsRepoPollIntervalSeconds",
		},
		{
			name: "appsRepoPollInterval - 59 is invalid (between 0 and 60)",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 59,
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appsRepoPollIntervalSeconds",
		},
		{
			name: "appsRepoPollInterval - 86401 is invalid (exceeds maximum)",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 86401,
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appsRepoPollIntervalSeconds",
		},
		// appSources uniqueness validation tests (Location + Scope)
		{
			name: "appSources - unique Location+Scope combinations are valid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "source1", Location: "/apps1", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local"}},
					{Name: "source2", Location: "/apps2", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local"}},
					{Name: "source3", Location: "/apps1", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "cluster"}},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "appSources - duplicate Location+Scope is invalid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "source1", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local", VolName: "vol1"}},
					{Name: "source2", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local", VolName: "vol2"}},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appSources[1]",
		},
		{
			name: "appSources - same location different scope is valid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "source1", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local"}},
					{Name: "source2", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "cluster"}},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "appSources - uses defaults for scope uniqueness check",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				Defaults: enterpriseApi.AppSourceDefaultSpec{Scope: "local"},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "source1", Location: "/apps"}, // uses default scope
					{Name: "source2", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local"}}, // explicit same scope
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appSources[1]",
		},
		{
			name: "appSources - multiple duplicates",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "source1", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local"}},
					{Name: "source2", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local"}},
					{Name: "source3", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local"}},
				},
			},
			wantErrCount: 2, // source2 and source3 are duplicates of source1
		},
		// premiumAppsProps validation tests
		{
			name: "premiumApps scope with premiumAppsProps.type is valid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "esApps",
						Location: "/es-apps",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							Scope:            "premiumApps",
							PremiumAppsProps: enterpriseApi.PremiumAppsProps{Type: "enterpriseSecurity"},
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "premiumApps scope without premiumAppsProps.type is invalid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "esApps",
						Location: "/es-apps",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							Scope: "premiumApps",
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appSources[0].premiumAppsProps.type",
		},
		{
			name: "premiumApps scope with premiumAppsProps.type from defaults is valid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				Defaults: enterpriseApi.AppSourceDefaultSpec{
					PremiumAppsProps: enterpriseApi.PremiumAppsProps{Type: "enterpriseSecurity"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "esApps",
						Location: "/es-apps",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							Scope: "premiumApps",
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "premiumApps scope from defaults without premiumAppsProps.type is invalid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				Defaults: enterpriseApi.AppSourceDefaultSpec{
					Scope: "premiumApps",
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "esApps",
						Location: "/es-apps",
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appFramework.appSources[0].premiumAppsProps.type",
		},
		{
			name: "non-premiumApps scope without premiumAppsProps is valid",
			appConfig: &enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "localApps",
						Location: "/apps",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							Scope: "local",
						},
					},
				},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateAppFramework(tt.appConfig, field.NewPath("spec").Child("appFramework"))

			if len(errs) != tt.wantErrCount {
				t.Errorf("validateAppFramework() got %d errors, want %d", len(errs), tt.wantErrCount)
				for _, e := range errs {
					t.Logf("  error: %s", e.Error())
				}
			}

			if tt.wantErrField != "" && len(errs) > 0 {
				found := false
				for _, e := range errs {
					if e.Field == tt.wantErrField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("validateAppFramework() expected error on field %s", tt.wantErrField)
				}
			}
		})
	}
}

func TestValidateStorageConfig(t *testing.T) {
	tests := []struct {
		name         string
		config       *enterpriseApi.StorageClassSpec
		wantErrCount int
		wantErrField string
	}{
		{
			name:         "empty config - valid",
			config:       &enterpriseApi.StorageClassSpec{},
			wantErrCount: 0,
		},
		{
			name: "valid storage capacity - 10Gi",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "10Gi",
				StorageClassName: "standard",
			},
			wantErrCount: 0,
		},
		{
			name: "valid storage capacity - 100Gi",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "100Gi",
				StorageClassName: "fast",
			},
			wantErrCount: 0,
		},
		{
			name: "invalid storage capacity - missing Gi suffix",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "10",
				StorageClassName: "standard",
			},
			wantErrCount: 1,
			wantErrField: "spec.storageCapacity",
		},
		{
			name: "invalid storage capacity - wrong suffix Mi",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "10Mi",
				StorageClassName: "standard",
			},
			wantErrCount: 1,
			wantErrField: "spec.storageCapacity",
		},
		{
			name: "invalid storage capacity - text value",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "large",
				StorageClassName: "standard",
			},
			wantErrCount: 1,
			wantErrField: "spec.storageCapacity",
		},
		{
			name: "missing storageClassName with persistent storage",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "10Gi",
				EphemeralStorage: false,
				StorageClassName: "",
			},
			wantErrCount: 1,
			wantErrField: "spec.storageClassName",
		},
		{
			name: "ephemeral storage - storageClassName not required",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "10Gi",
				EphemeralStorage: true,
				StorageClassName: "",
			},
			wantErrCount: 0,
		},
		{
			name: "multiple errors - invalid capacity and missing className",
			config: &enterpriseApi.StorageClassSpec{
				StorageCapacity:  "10MB",
				EphemeralStorage: false,
				StorageClassName: "",
			},
			wantErrCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateStorageConfig(tt.config, field.NewPath("spec"))

			if len(errs) != tt.wantErrCount {
				t.Errorf("validateStorageConfig() got %d errors, want %d", len(errs), tt.wantErrCount)
				for _, e := range errs {
					t.Logf("  error: %s", e.Error())
				}
			}

			if tt.wantErrField != "" && len(errs) > 0 {
				found := false
				for _, e := range errs {
					if e.Field == tt.wantErrField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("validateStorageConfig() expected error on field %s", tt.wantErrField)
				}
			}
		})
	}
}

func TestGetCommonWarnings(t *testing.T) {
	tests := []struct {
		name         string
		spec         *enterpriseApi.CommonSplunkSpec
		wantWarnings int
	}{
		{
			name:         "empty spec - no warnings",
			spec:         &enterpriseApi.CommonSplunkSpec{},
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := getCommonWarnings(tt.spec)

			if len(warnings) != tt.wantWarnings {
				t.Errorf("getCommonWarnings() got %d warnings, want %d", len(warnings), tt.wantWarnings)
			}
		})
	}
}
