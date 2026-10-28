// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func TestMajorUpgradeFactoryRejectsUnresolvableTargetBeforeConstruction(t *testing.T) {
	targetVersion := platformv1alpha1.PostgresMajorVersion18
	missingTarget := pgtestfixture.Images()
	delete(missingTarget.PostgreSQL, targetVersion)
	for _, cfg := range []*clustercore.MergedConfig{
		nil,
		{},
		{Spec: &platformv1alpha1.PostgresClusterSpec{PostgresVersion: &targetVersion}},
		{Spec: &platformv1alpha1.PostgresClusterSpec{PostgresVersion: &targetVersion, Images: missingTarget}},
	} {
		// No client is configured: invalid mappings must fail before adapters run.
		reconciler := &PostgresClusterReconciler{}
		useCase, err := reconciler.newMajorUpgradeUseCase(types.NamespacedName{}, nil, cfg)()
		require.ErrorContains(t, err, "resolve major-upgrade target image")
		assert.Nil(t, useCase)
	}
}

func TestBlueGreenUpgradePlansShareResolvedImageAndFreezeConfiguration(t *testing.T) {
	targetVersion := platformv1alpha1.PostgresMajorVersion18
	cfg := &clustercore.MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{
		PostgresVersion: &targetVersion,
		Images:          pgtestfixture.Images(),
		Instances:       ptr.To(int32(3)),
	}}
	targetImage, err := clustercore.EffectivePostgresImage(cfg)
	require.NoError(t, err)
	preflight, candidate := blueGreenUpgradePlans(cfg, targetImage)
	assert.Equal(t, targetImage, preflight.TargetImage)
	assert.Equal(t, targetImage, candidate.TargetImage)
	assert.Equal(t, int(*cfg.Spec.Instances), candidate.TargetInstances)
	delete(cfg.Spec.Images.PostgreSQL, targetVersion)
	assert.Equal(t, targetImage, string(preflight.TargetConfiguration.Images.PostgreSQL[targetVersion]))
	assert.Equal(t, targetImage, string(candidate.TargetConfiguration.Images.PostgreSQL[targetVersion]))
}
