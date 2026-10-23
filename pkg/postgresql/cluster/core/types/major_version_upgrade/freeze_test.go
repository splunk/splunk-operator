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
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFrozenBlueGreenClusterSpecExcludesGates(t *testing.T) {
	timeout := &metav1.Duration{Duration: time.Minute}
	spec := platformv1alpha1.PostgresClusterSpec{
		PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{
			Allow: new(true),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{
				Switchover:        new(true),
				Cleanup:           new(true),
				Cancel:            new(true),
				SwitchoverTimeout: timeout,
			},
		},
	}

	frozen := FrozenBlueGreenClusterSpec(spec)
	assert.Nil(t, frozen.PostgresMajorUpgradeConfig.Allow)
	assert.Nil(t, frozen.PostgresMajorUpgradeConfig.BlueGreen.Switchover)
	assert.Nil(t, frozen.PostgresMajorUpgradeConfig.BlueGreen.Cleanup)
	assert.Nil(t, frozen.PostgresMajorUpgradeConfig.BlueGreen.Cancel)
	assert.Equal(t, timeout, frozen.PostgresMajorUpgradeConfig.BlueGreen.SwitchoverTimeout)
	assert.NotNil(t, spec.PostgresMajorUpgradeConfig.Allow, "projection must not mutate the user spec")
}
