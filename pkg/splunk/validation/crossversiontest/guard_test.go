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

package crossversiontest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApiV4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestCrossVersionFieldProtection(t *testing.T) {
	if apiServerClient == nil {
		t.Skip("KUBEBUILDER_ASSETS is not set; run via `make test` or `make setup-envtest`")
	}

	tests := []struct {
		name      string
		existing  []client.Object
		write     func(ctx context.Context, c client.Client) error
		wantError string
	}{
		{
			name: "v3 update of a noah indexercluster is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "update"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				obj := &enterpriseApiV3.IndexerCluster{}
				if err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "update"}, obj); err != nil {
					return err
				}
				obj.Labels = map[string]string{"touched": "true"}
				return c.Update(ctx, obj)
			},
			wantError: "IndexerCluster default/update cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 merge patch is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "merge-patch"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Patch(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "merge-patch"}}, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":{"touched":"true"}}}`)))
			},
			wantError: "IndexerCluster default/merge-patch cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 json patch is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "json-patch"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Patch(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "json-patch"}}, client.RawPatch(types.JSONPatchType, []byte(`[{"op":"add","path":"/metadata/labels","value":{"touched":"true"}}]`)))
			},
			wantError: "IndexerCluster default/json-patch cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 server-side apply is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "apply"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Patch(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "apply"}}, client.RawPatch(types.ApplyPatchType, []byte(`{"apiVersion":"enterprise.splunk.com/v3","kind":"IndexerCluster","metadata":{"name":"apply","namespace":"default","labels":{"touched":"true"}}}`)), client.FieldOwner("crossversiontest"), client.ForceOwnership)
			},
			wantError: "IndexerCluster default/apply cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 status update is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "status"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				obj := &enterpriseApiV3.IndexerCluster{}
				if err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "status"}, obj); err != nil {
					return err
				}
				obj.Status.Phase = enterpriseApiV4.PhaseReady
				return c.Status().Update(ctx, obj)
			},
			wantError: "IndexerCluster default/status cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 scale is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "scale"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.SubResource("scale").Update(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "scale"}}, client.WithSubResourceBody(&autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: 5}}))
			},
			wantError: "IndexerCluster default/scale cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 delete of an object with a finalizer is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "finalizer-delete", Finalizers: []string{"enterprise.splunk.com/delete-pvc"}},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "finalizer-delete"}})
			},
			wantError: "IndexerCluster default/finalizer-delete cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 foreground delete is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "foreground-delete"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "foreground-delete"}}, client.PropagationPolicy(metav1.DeletePropagationForeground))
			},
			wantError: "IndexerCluster default/foreground-delete cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 orphan delete is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "orphan-delete"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "orphan-delete"}}, client.PropagationPolicy(metav1.DeletePropagationOrphan))
			},
			wantError: "IndexerCluster default/orphan-delete cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 deletecollection is denied",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "deletecollection", Finalizers: []string{"enterprise.splunk.com/delete-pvc"}},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.DeleteAllOf(ctx, &enterpriseApiV3.IndexerCluster{}, client.InNamespace("default"))
			},
			wantError: "IndexerCluster default/deletecollection cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 merge patch of a noah searchheadcluster is denied",
			existing: []client.Object{
				&enterpriseApiV4.SearchHeadCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "shc"},
					Spec:       enterpriseApiV4.SearchHeadClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Patch(ctx, &enterpriseApiV3.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "shc"}}, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":{"touched":"true"}}}`)))
			},
			wantError: "SearchHeadCluster default/shc cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "v3 update of a classic indexercluster is allowed",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "classic-update"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, CommonSplunkSpec: enterpriseApiV4.CommonSplunkSpec{ClusterManagerRef: corev1.ObjectReference{Name: "cm"}}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				obj := &enterpriseApiV3.IndexerCluster{}
				if err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "classic-update"}, obj); err != nil {
					return err
				}
				obj.Labels = map[string]string{"touched": "true"}
				return c.Update(ctx, obj)
			},
		},
		{
			name: "v3 scale of a classic indexercluster is allowed",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "classic-scale"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, CommonSplunkSpec: enterpriseApiV4.CommonSplunkSpec{ClusterManagerRef: corev1.ObjectReference{Name: "cm"}}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.SubResource("scale").Update(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "classic-scale"}}, client.WithSubResourceBody(&autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: 5}}))
			},
		},
		{
			name: "v3 delete of a classic indexercluster with a finalizer is allowed",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "classic-delete", Finalizers: []string{"enterprise.splunk.com/delete-pvc"}},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, CommonSplunkSpec: enterpriseApiV4.CommonSplunkSpec{ClusterManagerRef: corev1.ObjectReference{Name: "cm"}}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "classic-delete"}})
			},
		},
		{
			name: "v3 background delete of a noah indexercluster is allowed",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "background-delete"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, &enterpriseApiV3.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "background-delete"}})
			},
		},
		{
			name: "v3 create is allowed",
			write: func(ctx context.Context, c client.Client) error {
				return c.Create(ctx, &enterpriseApiV3.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "v3-create"},
					Spec:       enterpriseApiV3.IndexerClusterSpec{Replicas: 3, CommonSplunkSpec: enterpriseApiV4.CommonSplunkSpec{ClusterManagerRef: corev1.ObjectReference{Name: "cm"}}},
				})
			},
		},
		{
			name: "v4 update of a noah indexercluster is allowed",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "v4-update"},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				obj := &enterpriseApiV4.IndexerCluster{}
				if err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "v4-update"}, obj); err != nil {
					return err
				}
				obj.Labels = map[string]string{"touched": "true"}
				return c.Update(ctx, obj)
			},
		},
		{
			name: "v4 delete of a noah indexercluster with a finalizer is allowed",
			existing: []client.Object{
				&enterpriseApiV4.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "v4-delete", Finalizers: []string{"enterprise.splunk.com/delete-pvc"}},
					Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3, NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				},
			},
			write: func(ctx context.Context, c client.Client) error {
				return c.Delete(ctx, &enterpriseApiV4.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "v4-delete"}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				for _, list := range []client.ObjectList{&enterpriseApiV4.IndexerClusterList{}, &enterpriseApiV4.SearchHeadClusterList{}} {
					_ = apiServerClient.List(context.Background(), list, client.InNamespace("default"))
					_ = meta.EachListItem(list, func(o runtime.Object) error {
						obj := o.(client.Object)
						if len(obj.GetFinalizers()) > 0 {
							obj.SetFinalizers(nil)
							_ = apiServerClient.Update(context.Background(), obj)
						}
						_ = apiServerClient.Delete(context.Background(), obj)
						return nil
					})
				}
			})

			var created []client.Object
			for _, obj := range tt.existing {
				require.NoError(t, apiServerClient.Create(t.Context(), obj))
				created = append(created, obj.DeepCopyObject().(client.Object))
			}

			err := tt.write(t.Context(), apiServerClient)

			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			// Both webhook entries deny a v3 update with this message; the API server prefixes it with
			// whichever entry answered first, so only the message is matched.
			require.ErrorContains(t, err, tt.wantError)
			for _, obj := range created {
				stored := obj.DeepCopyObject().(client.Object)
				require.NoError(t, apiServerClient.Get(t.Context(), client.ObjectKeyFromObject(obj), stored))
				assert.Equal(t, obj.GetResourceVersion(), stored.GetResourceVersion(), "a denied write changed the stored object")
			}
		})
	}
}
