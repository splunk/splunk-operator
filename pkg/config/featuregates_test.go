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

	"k8s.io/component-base/featuregate"
)

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
