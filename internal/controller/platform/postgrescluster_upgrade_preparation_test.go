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

package controller

import (
	platform "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
	"testing"
)

func TestBlueGreenPlansCopyResolvedConfiguration(t *testing.T) {
	cfg := &core.MergedConfig{Spec: &platform.PostgresClusterSpec{PostgresVersion: ptr.To("18"), Instances: ptr.To(int32(3))}}
	image := "registry.example.invalid/postgresql:18-test"
	preflight, candidate := blueGreenUpgradePlans(cfg, image)
	assert.Equal(t, image, preflight.TargetImage)
	assert.Equal(t, image, candidate.TargetImage)
	assert.Equal(t, 3, candidate.TargetInstances)
	cfg.Spec.PostgresVersion = ptr.To("17")
	assert.Equal(t, "18", *preflight.TargetConfiguration.PostgresVersion)
	assert.Equal(t, "18", *candidate.TargetConfiguration.PostgresVersion)
}
