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

package majorversionupgradetypes

import (
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FrozenBlueGreenClusterSpec returns the declarative configuration that must
// remain stable once source preparation starts. Gates are deliberate runtime
// permissions, not configuration drift: changing one must not invalidate an
// active attempt or its persisted baseline.
func FrozenBlueGreenClusterSpec(spec platformv1alpha1.PostgresClusterSpec) platformv1alpha1.PostgresClusterSpec {
	frozen := *spec.DeepCopy()
	if frozen.PostgresMajorUpgradeConfig == nil {
		return frozen
	}

	config := frozen.PostgresMajorUpgradeConfig.DeepCopy()
	config.Allow = nil
	if config.BlueGreen == nil {
		config.BlueGreen = &platformv1alpha1.PostgresBlueGreenUpgradeConfig{}
	}
	blueGreen := config.BlueGreen.DeepCopy()
	if blueGreen.SwitchoverTimeout == nil {
		blueGreen.SwitchoverTimeout = &metav1.Duration{Duration: platformv1alpha1.DefaultBlueGreenSwitchoverTimeout}
	}
	blueGreen.Switchover = nil
	blueGreen.Cleanup = nil
	blueGreen.Cancel = nil
	config.BlueGreen = blueGreen
	frozen.PostgresMajorUpgradeConfig = config
	return frozen
}
