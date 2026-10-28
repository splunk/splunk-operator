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

package webhook_test

import (
	"testing"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	webhook "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/webhook"
	"github.com/splunk/splunk-operator/pkg/splunk/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestPostgresClusterImageMappingAdmission(t *testing.T) {
	source := platformApi.PostgresMajorVersion17
	target := platformApi.PostgresMajorVersion18
	class := &platformApi.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "images"}, Spec: platformApi.PostgresClusterClassSpec{Config: &platformApi.PostgresClusterClassConfig{Instances: ptr.To(int32(1)), Storage: ptr.To(resource.MustParse("1Gi")), PostgresVersion: &source}}}
	reader := newFakeReader(class).Build()
	server := validation.NewWebhookServer(validation.WebhookServerOptions{Validators: validation.DefaultValidators, Client: reader})
	cluster := &platformApi.PostgresCluster{TypeMeta: metav1.TypeMeta{APIVersion: platformApi.GroupVersion.String(), Kind: "PostgresCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}, Spec: platformApi.PostgresClusterSpec{Class: class.Name, Images: pgtestfixture.Images()}}
	for _, tt := range []struct {
		name    string
		mutate  func(*platformApi.PostgresCluster)
		allowed bool
	}{
		{"inherits version", func(*platformApi.PostgresCluster) {}, true},
		{"missing images", func(p *platformApi.PostgresCluster) { p.Spec.Images = nil }, false},
		{"missing inherited major", func(p *platformApi.PostgresCluster) { delete(p.Spec.Images.PostgreSQL, source) }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := cluster.DeepCopy()
			tt.mutate(current)
			response := sendAdmissionReview(t, server, newPostgresClusterAdmissionReview(t, "images", admissionv1.Create, current, nil))
			assert.Equal(t, tt.allowed, response.Allowed)
		})
	}
	t.Run("target mapping and version update are atomic", func(t *testing.T) {
		old := cluster.DeepCopy()
		old.Spec.PostgresVersion = &source
		delete(old.Spec.Images.PostgreSQL, target)
		next := old.DeepCopy()
		next.Spec.PostgresVersion = &target
		response := sendAdmissionReview(t, server, newPostgresClusterAdmissionReview(t, "images-update", admissionv1.Update, next, old))
		require.False(t, response.Allowed)
		next.Spec.Images.PostgreSQL[target] = pgtestfixture.Images().PostgreSQL[target]
		response = sendAdmissionReview(t, server, newPostgresClusterAdmissionReview(t, "images-update", admissionv1.Update, next, old))
		assert.True(t, response.Allowed)
	})
	t.Run("inherited pooling requires PgBouncer", func(t *testing.T) {
		pooled := class.DeepCopy()
		pooled.Spec.Config.ConnectionPooler = &platformApi.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}
		current := cluster.DeepCopy()
		current.Spec.Images.PgBouncer = ""
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), current, newFakeReader(pooled).Build())
		require.NotEmpty(t, errs)
		assert.Contains(t, errs.ToAggregate().Error(), "spec.images.pgbouncer")
		current.Spec.ConnectionPooler = &platformApi.ConnectionPoolerEnableConfig{Enabled: ptr.To(false)}
		assert.Empty(t, webhook.ValidatePostgresClusterCreate(t.Context(), current, newFakeReader(pooled).Build()))
	})
}
