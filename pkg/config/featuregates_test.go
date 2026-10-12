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

package config

import (
	"os"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/component-base/featuregate"
)

func TestFeatureGateRegistration(t *testing.T) {
	tests := []struct {
		gate featuregate.Feature
		want featuregate.FeatureSpec
	}{
		{gate: CrossVersionFieldProtection, want: featuregate.FeatureSpec{Default: true, PreRelease: featuregate.Beta}},
	}

	for _, tt := range tests {
		t.Run(string(tt.gate), func(t *testing.T) {
			spec, ok := DefaultMutableFeatureGate.GetAll()[tt.gate]
			require.True(t, ok, "%s not registered", tt.gate)
			assert.Equal(t, tt.want.Default, spec.Default, "default")
			assert.Equal(t, tt.want.PreRelease, spec.PreRelease, "prerelease")
			assert.Equal(t, tt.want.LockToDefault, spec.LockToDefault, "lockToDefault")
			assert.Equal(t, tt.want.Default, DefaultMutableFeatureGate.Enabled(tt.gate), "enabled")
		})
	}
}

func TestApplyFeatureGatesEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		flag    string
		wantErr string
		want    map[featuregate.Feature]bool
	}{
		{
			name: "empty keeps defaults",
			env:  "",
			want: map[featuregate.Feature]bool{ValidationWebhook: false, CrossVersionFieldProtection: true},
		},
		{
			name: "sets gates, ignoring spaces and empty entries",
			env:  " ValidationWebhook = true , CrossVersionFieldProtection=false,",
			want: map[featuregate.Feature]bool{ValidationWebhook: true, CrossVersionFieldProtection: false},
		},
		{
			name: "leaves gates it does not name",
			env:  "ValidationWebhook=true",
			want: map[featuregate.Feature]bool{ValidationWebhook: true, CrossVersionFieldProtection: true},
		},
		{
			name: "flag wins for a gate set in both, env-only gates keep the env value",
			env:  "ValidationWebhook=true,CrossVersionFieldProtection=false",
			flag: "--feature-gates=CrossVersionFieldProtection=true",
			want: map[featuregate.Feature]bool{ValidationWebhook: true, CrossVersionFieldProtection: true},
		},
		{
			name:    "unknown gate",
			env:     "NoSuchGate=true",
			wantErr: `invalid FEATURE_GATES "NoSuchGate=true": unrecognized feature gate: NoSuchGate`,
		},
		{
			name:    "missing value",
			env:     "ValidationWebhook",
			wantErr: `invalid FEATURE_GATES "ValidationWebhook": missing bool value for ValidationWebhook`,
		},
		{
			name:    "non-bool value",
			env:     "ValidationWebhook=yes please",
			wantErr: `invalid FEATURE_GATES "ValidationWebhook=yes please": invalid value of ValidationWebhook=yes please, err: strconv.ParseBool: parsing "yes please": invalid syntax`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fg := featuregate.NewFeatureGate()
			require.NoError(t, fg.Add(defaultFeatureGates))
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			fg.AddFlag(fs)

			t.Setenv(FeatureGatesEnvVar, tt.env)

			err := ApplyFeatureGatesEnv(fg)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.flag != "" {
				require.NoError(t, fs.Parse([]string{tt.flag}))
			}

			for gate, want := range tt.want {
				assert.Equal(t, want, fg.Enabled(gate), string(gate))
			}
		})
	}
}

func TestValidationWebhookRegistered(t *testing.T) {
	all := DefaultMutableFeatureGate.GetAll()
	spec, ok := all[ValidationWebhook]
	if !ok {
		t.Fatal("ValidationWebhook gate not registered")
	}
	if spec.Default != false {
		t.Errorf("ValidationWebhook default: got %v, want false", spec.Default)
	}
	if spec.PreRelease != featuregate.Alpha {
		t.Errorf("ValidationWebhook prerelease: got %v, want Alpha", spec.PreRelease)
	}
}

func TestValidationWebhookOffByDefault(t *testing.T) {
	if DefaultMutableFeatureGate.Enabled(ValidationWebhook) {
		t.Error("ValidationWebhook should be disabled by default (Alpha)")
	}
}

func TestLegacyEnvVarEnablesGate(t *testing.T) {
	fg := featuregate.NewFeatureGate()
	if err := fg.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		ValidationWebhook: {Default: false, PreRelease: featuregate.Alpha},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	t.Setenv("ENABLE_VALIDATION_WEBHOOK", "true")
	applyLegacyValidationWebhookEnv(fg)

	if !fg.Enabled(ValidationWebhook) {
		t.Error("ValidationWebhook should be enabled when ENABLE_VALIDATION_WEBHOOK=true")
	}
}

func TestLegacyEnvVarIgnoredWhenUnset(t *testing.T) {
	fg := featuregate.NewFeatureGate()
	if err := fg.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		ValidationWebhook: {Default: false, PreRelease: featuregate.Alpha},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	os.Unsetenv("ENABLE_VALIDATION_WEBHOOK")
	applyLegacyValidationWebhookEnv(fg)

	if fg.Enabled(ValidationWebhook) {
		t.Error("ValidationWebhook should remain disabled when env var is not set")
	}
}

func TestLegacyEnvVarIgnoredWhenNotTrue(t *testing.T) {
	fg := featuregate.NewFeatureGate()
	if err := fg.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		ValidationWebhook: {Default: false, PreRelease: featuregate.Alpha},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	t.Setenv("ENABLE_VALIDATION_WEBHOOK", "false")
	applyLegacyValidationWebhookEnv(fg)

	if fg.Enabled(ValidationWebhook) {
		t.Error("ValidationWebhook should remain disabled when ENABLE_VALIDATION_WEBHOOK=false")
	}
}
