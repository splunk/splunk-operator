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

package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateEffectiveImages(t *testing.T) {
	for _, tc := range []struct {
		name           string
		major          string
		pooling        bool
		missingMapping bool
		missingPooler  bool
		missingImages  bool
		wantField      string
	}{
		{name: "mapped class major", major: platformv1alpha1.PostgresMajorVersion17},
		{name: "mapped class minor", major: platformv1alpha1.PostgresMajorVersion17 + ".1"},
		{name: "class not resolved"},
		{name: "inherited major missing", major: platformv1alpha1.PostgresMajorVersion18, missingMapping: true, wantField: "spec.images.postgresql[18]"},
		{name: "inherited major without images", major: platformv1alpha1.PostgresMajorVersion17, missingImages: true, wantField: "spec.images.postgresql[17]"},
		{name: "inherited pooling", pooling: true},
		{name: "inherited pooling missing image", pooling: true, missingPooler: true, wantField: "spec.images.pgbouncer"},
		{name: "inherited pooling without images", pooling: true, missingImages: true, wantField: "spec.images.pgbouncer"},
		{name: "pooling disabled", missingPooler: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			images := pgtestfixture.Images()
			if tc.missingMapping {
				delete(images.PostgreSQL, tc.major)
			}
			if tc.missingPooler {
				images.PgBouncer = ""
			}
			if tc.missingImages {
				images = nil
			}
			var version *string
			if tc.major != "" {
				version = &tc.major
			}
			errs := ValidateEffectiveImages(images, version, tc.pooling)
			if tc.wantField == "" {
				require.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			assert.Equal(t, tc.wantField, errs[0].Field)
		})
	}
}

func TestResolvePostgresImage(t *testing.T) {
	images := pgtestfixture.Images()
	for major, image := range images.PostgreSQL {
		for _, version := range []string{major, major + ".1"} {
			actual, err := ResolvePostgresImage(images, version)
			require.NoError(t, err)
			assert.Equal(t, string(image), actual)
		}
	}
	delete(images.PostgreSQL, platformv1alpha1.PostgresMajorVersion18)
	for _, tc := range []struct {
		images  *platformv1alpha1.PostgresClusterImages
		version string
	}{
		{nil, platformv1alpha1.PostgresMajorVersion17}, {images, platformv1alpha1.PostgresMajorVersion18}, {images, "99"},
	} {
		image, err := ResolvePostgresImage(tc.images, tc.version)
		require.Error(t, err)
		assert.Empty(t, image)
	}
	_, err := EffectivePostgresImage(nil)
	require.Error(t, err)
}

func TestPoolerBuilderRequiresItsImage(t *testing.T) {
	scheme := newTestScheme()
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "test"}}
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{Images: pgtestfixture.Images()},
		CNPG: &platformv1alpha1.CNPGConfig{ConnectionPooler: &platformv1alpha1.ConnectionPoolerConfig{
			Instances: ptr.To(int32(1)), Mode: ptr.To(platformv1alpha1.ConnectionPoolerModeTransaction),
		}},
	}
	// PostgreSQL mapping validation belongs to the merged-config boundary.
	cfg.Spec.Images.PostgreSQL = nil
	cnpg := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name}}
	pooler, err := buildCNPGPooler(scheme, cluster, cfg, cnpg, readWriteEndpoint, false, testEnvironmentNamer)
	require.NoError(t, err)
	assert.Equal(t, string(cfg.Spec.Images.PgBouncer), pooler.Spec.Template.Spec.Containers[0].Image)
	for _, image := range []platformv1alpha1.PinnedImageReference{""} {
		cfg.Spec.Images.PgBouncer = image
		pooler, err = buildCNPGPooler(scheme, cluster, cfg, cnpg, readWriteEndpoint, false, testEnvironmentNamer)
		require.ErrorContains(t, err, "spec.images.pgbouncer")
		assert.NotContains(t, err.Error(), "spec.images.postgresql")
		assert.Nil(t, pooler)
	}
	cfg.Spec.Images = nil
	_, err = buildCNPGPooler(scheme, cluster, cfg, cnpg, readWriteEndpoint, false, testEnvironmentNamer)
	require.ErrorContains(t, err, "cluster image configuration is required")
}

type imagePatchCounter struct {
	client.Client
	patches int
}

func (c *imagePatchCounter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.patches++
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func TestPoolerImageMappingDrift(t *testing.T) {
	scheme := newTestScheme()
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default", UID: "pg-uid"}}
	environment := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace}}
	cfg := &MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{Images: pgtestfixture.Images()}, CNPG: &platformv1alpha1.CNPGConfig{ConnectionPooler: &platformv1alpha1.ConnectionPoolerConfig{Instances: ptr.To(int32(1)), Mode: ptr.To(platformv1alpha1.ConnectionPoolerModeTransaction)}}}
	cfg.Spec.Images.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "registry-reader"}}
	for _, endpoint := range []string{readWriteEndpoint, readOnlyEndpoint} {
		t.Run(endpoint, func(t *testing.T) {
			c := &imagePatchCounter{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
			reconcile := func() {
				t.Helper()
				require.NoError(t, createAndUpdateConnectionPooler(t.Context(), c, scheme, cluster, cfg, environment, endpoint, false, testEnvironmentNamer))
			}
			key := client.ObjectKey{Namespace: cluster.Namespace, Name: poolerResourceName(cluster.Name, endpoint)}
			pooler := &cnpgv1.Pooler{}
			reconcile()
			require.NoError(t, c.Get(t.Context(), key, pooler))
			assert.Equal(t, string(cfg.Spec.Images.PgBouncer), pooler.Spec.Template.Spec.Containers[0].Image)
			assert.Equal(t, cfg.Spec.Images.ImagePullSecrets, pooler.Spec.Template.Spec.ImagePullSecrets)
			for _, mutate := range []func(*cnpgv1.Pooler){
				func(p *cnpgv1.Pooler) {
					p.Spec.Template.Spec.Containers[0].Image = pgtestfixture.ImageForVersion(platformv1alpha1.PostgresMajorVersion17)
				},
				func(p *cnpgv1.Pooler) {
					prefix, _, _ := strings.Cut(p.Spec.Template.Spec.Containers[0].Image, "@")
					p.Spec.Template.Spec.Containers[0].Image = fmt.Sprintf("%s@sha256:%x", prefix, sha256.Sum256([]byte("external-digest")))
				},
				func(p *cnpgv1.Pooler) { p.Spec.Template.Spec.ImagePullSecrets = nil },
			} {
				require.NoError(t, c.Get(t.Context(), key, pooler))
				mutate(pooler)
				require.NoError(t, c.Update(t.Context(), pooler))
				before := c.patches
				reconcile()
				assert.Equal(t, before+1, c.patches)
				require.NoError(t, c.Get(t.Context(), key, pooler))
				assert.Equal(t, string(cfg.Spec.Images.PgBouncer), pooler.Spec.Template.Spec.Containers[0].Image)
				assert.Equal(t, cfg.Spec.Images.ImagePullSecrets, pooler.Spec.Template.Spec.ImagePullSecrets)
				before = c.patches
				reconcile()
				assert.Equal(t, before, c.patches, "converged pooler must not be patched again")
			}
			cfg.Spec.Images.ImagePullSecrets = nil
			reconcile()
			require.NoError(t, c.Get(t.Context(), key, pooler))
			assert.Empty(t, pooler.Spec.Template.Spec.ImagePullSecrets)
			cfg.Spec.Images.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "registry-reader"}}
		})
	}
}
