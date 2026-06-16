/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"testing"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newClusterWatchScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	return scheme
}

func TestExtractExternalSuperuserSecretName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "non-PostgresCluster object yields nil",
			obj:  &corev1.Secret{},
			want: nil,
		},
		{
			name: "no PasswordConfig yields nil",
			obj:  &enterprisev4.PostgresCluster{},
			want: nil,
		},
		{
			name: "empty SuperuserExternalSecretRef.Name yields nil",
			obj: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					PasswordConfig: &enterprisev4.SuperuserPasswordConfig{},
				},
			},
			want: nil,
		},
		{
			name: "external ref name is indexed",
			obj: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					PasswordConfig: &enterprisev4.SuperuserPasswordConfig{
						SuperuserExternalSecretRef: corev1.LocalObjectReference{Name: "ext-sup"},
					},
				},
			},
			want: []string{"ext-sup"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractExternalSuperuserSecretName(tc.obj))
		})
	}
}

func TestEnqueueClustersForExternalSecret(t *testing.T) {
	t.Parallel()
	scheme := newClusterWatchScheme(t)

	const (
		ns         = "default"
		extSecret  = "ext-sup"
		otherNS    = "other-ns"
		ownedName  = "owned-secret"
		unrelated  = "unrelated-secret"
		clusterRef = "pg1"
	)

	matchingPC := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRef, Namespace: ns},
		Spec: enterprisev4.PostgresClusterSpec{
			PasswordConfig: &enterprisev4.SuperuserPasswordConfig{
				SuperuserExternalSecretRef: corev1.LocalObjectReference{Name: extSecret},
			},
		},
	}
	internalPC := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "internal-pg", Namespace: ns},
	}
	otherNSPC := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg-elsewhere", Namespace: otherNS},
		Spec: enterprisev4.PostgresClusterSpec{
			PasswordConfig: &enterprisev4.SuperuserPasswordConfig{
				SuperuserExternalSecretRef: corev1.LocalObjectReference{Name: extSecret},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&enterprisev4.PostgresCluster{}, indexExternalSuperuserSecret, extractExternalSuperuserSecretName).
		WithObjects(matchingPC, internalPC, otherNSPC).
		Build()

	r := &PostgresClusterReconciler{Client: fakeClient}

	t.Run("matching external secret enqueues the cluster", func(t *testing.T) {
		reqs := r.enqueueClustersForExternalSecret(context.Background(),
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: extSecret, Namespace: ns}})
		require.Len(t, reqs, 1)
		assert.Equal(t, clusterRef, reqs[0].Name)
		assert.Equal(t, ns, reqs[0].Namespace)
	})

	t.Run("namespace isolation: same name in a different namespace does not enqueue", func(t *testing.T) {
		reqs := r.enqueueClustersForExternalSecret(context.Background(),
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: extSecret, Namespace: "another-ns"}})
		assert.Empty(t, reqs)
	})

	t.Run("unrelated secret does not enqueue any cluster", func(t *testing.T) {
		reqs := r.enqueueClustersForExternalSecret(context.Background(),
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: unrelated, Namespace: ns}})
		assert.Empty(t, reqs)
	})

	t.Run("owned secret is skipped — Owns() handles those", func(t *testing.T) {
		owned := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: ownedName, Namespace: ns,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: enterprisev4.GroupVersion.String(),
					Kind:       "PostgresCluster",
					Name:       clusterRef,
					Controller: ptr.To(true),
					UID:        "uid-owned",
				}},
			},
		}
		reqs := r.enqueueClustersForExternalSecret(context.Background(), owned)
		assert.Empty(t, reqs, "mapper must skip Secrets with a controller owner")
	})

	t.Run("ESO-owned secret (foreign controller) is still mapped", func(t *testing.T) {
		esoOwned := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: extSecret, Namespace: ns,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "external-secrets.io/v1",
					Kind:       "ExternalSecret",
					Name:       "ext-eso",
					Controller: ptr.To(true),
					UID:        "uid-eso",
				}},
			},
		}
		reqs := r.enqueueClustersForExternalSecret(context.Background(), esoOwned)
		require.Len(t, reqs, 1, "ESO-owned external secret must enqueue the referencing cluster")
		assert.Equal(t, clusterRef, reqs[0].Name)
		assert.Equal(t, ns, reqs[0].Namespace)
	})

	t.Run("non-Secret object yields nil", func(t *testing.T) {
		reqs := r.enqueueClustersForExternalSecret(context.Background(), &corev1.ConfigMap{})
		assert.Nil(t, reqs)
	})
}
