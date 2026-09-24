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
	"sort"
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

func newDatabaseWatchScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	return scheme
}

func dbWithRefs(name, ns string, refs ...[2]string) *enterprisev4.PostgresDatabase {
	dbs := make([]enterprisev4.DatabaseDefinition, 0, len(refs))
	for i, pair := range refs {
		dbs = append(dbs, enterprisev4.DatabaseDefinition{
			Name: name + "-db-" + string(rune('a'+i)),
			PasswordConfig: &enterprisev4.PasswordConfig{
				ExternalAdminSecretRef: corev1.LocalObjectReference{Name: pair[0]},
				ExternalRWSecretRef:    corev1.LocalObjectReference{Name: pair[1]},
			},
		})
	}
	return &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       enterprisev4.PostgresDatabaseSpec{Databases: dbs},
	}
}

func TestExtractExternalRoleSecretNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "non-PostgresDatabase object yields nil",
			obj:  &corev1.Secret{},
			want: nil,
		},
		{
			name: "no databases yields nil",
			obj:  &enterprisev4.PostgresDatabase{},
			want: nil,
		},
		{
			name: "internal-mode database yields nil",
			obj: &enterprisev4.PostgresDatabase{
				Spec: enterprisev4.PostgresDatabaseSpec{
					Databases: []enterprisev4.DatabaseDefinition{{Name: "db1"}},
				},
			},
			want: nil,
		},
		{
			name: "single external pair indexes both names",
			obj:  dbWithRefs("primary", "ns", [2]string{"adm-1", "rw-1"}),
			want: []string{"adm-1", "rw-1"},
		},
		{
			name: "shared admin/RW across two databases is de-duplicated",
			obj: dbWithRefs("primary", "ns",
				[2]string{"shared-adm", "shared-rw"},
				[2]string{"shared-adm", "shared-rw"},
			),
			want: []string{"shared-adm", "shared-rw"},
		},
		{
			name: "empty ref names are filtered out so they never enter the index",
			obj: dbWithRefs("primary", "ns",
				[2]string{"", ""},
				[2]string{"adm-only", ""},
			),
			want: []string{"adm-only"},
		},
		{
			name: "mixed internal+external databases only emit external names",
			obj: &enterprisev4.PostgresDatabase{
				Spec: enterprisev4.PostgresDatabaseSpec{
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "internal-db"}, // PasswordConfig nil
						{Name: "external-db", PasswordConfig: &enterprisev4.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{Name: "adm-x"},
							ExternalRWSecretRef:    corev1.LocalObjectReference{Name: "rw-x"},
						}},
					},
				},
			},
			want: []string{"adm-x", "rw-x"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractExternalRoleSecretNames(tc.obj)
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			assert.Equal(t, want, got)
		})
	}
}

func TestEnqueuePostgresDatabasesForExternalSecret(t *testing.T) {
	t.Parallel()
	scheme := newDatabaseWatchScheme(t)

	const (
		ns        = "default"
		otherNS   = "other-ns"
		admSecret = "shared-adm"
		rwSecret  = "shared-rw"
	)

	pd1 := dbWithRefs("pd-one", ns, [2]string{admSecret, rwSecret})
	pd2 := dbWithRefs("pd-two", ns, [2]string{admSecret, "rw-two"})
	pdOther := dbWithRefs("pd-elsewhere", otherNS, [2]string{admSecret, rwSecret})
	pdInternal := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "pd-internal", Namespace: ns},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{{Name: "x"}},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&enterprisev4.PostgresDatabase{}, indexExternalRoleSecrets, extractExternalRoleSecretNames).
		WithObjects(pd1, pd2, pdOther, pdInternal).
		Build()

	r := &PostgresDatabaseReconciler{Client: fakeClient}

	t.Run("shared admin secret enqueues every referencing PD in-namespace", func(t *testing.T) {
		reqs := r.enqueuePostgresDatabasesForExternalSecret(context.Background(),
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: admSecret, Namespace: ns}})
		require.Len(t, reqs, 2)
		got := []string{reqs[0].Name, reqs[1].Name}
		sort.Strings(got)
		assert.Equal(t, []string{"pd-one", "pd-two"}, got)
	})

	t.Run("rw-only secret enqueues only the matching PD", func(t *testing.T) {
		reqs := r.enqueuePostgresDatabasesForExternalSecret(context.Background(),
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rw-two", Namespace: ns}})
		require.Len(t, reqs, 1)
		assert.Equal(t, "pd-two", reqs[0].Name)
	})

	t.Run("namespace isolation: same name in a different namespace does not enqueue", func(t *testing.T) {
		reqs := r.enqueuePostgresDatabasesForExternalSecret(context.Background(),
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: admSecret, Namespace: "yet-another-ns"}})
		assert.Empty(t, reqs)
	})

	t.Run("unrelated secret does not enqueue any database", func(t *testing.T) {
		reqs := r.enqueuePostgresDatabasesForExternalSecret(context.Background(),
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns}})
		assert.Empty(t, reqs)
	})

	t.Run("owned secret is skipped — Owns() handles those", func(t *testing.T) {
		owned := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: admSecret, Namespace: ns,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: enterprisev4.GroupVersion.String(),
					Kind:       "PostgresDatabase",
					Name:       "pd-one",
					Controller: ptr.To(true),
					UID:        "uid-owned",
				}},
			},
		}
		reqs := r.enqueuePostgresDatabasesForExternalSecret(context.Background(), owned)
		assert.Empty(t, reqs, "mapper must skip Secrets with a controller owner")
	})

	t.Run("ESO-owned secret (foreign controller) is still mapped", func(t *testing.T) {
		esoOwned := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: admSecret, Namespace: ns,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "external-secrets.io/v1",
					Kind:       "ExternalSecret",
					Name:       "adm-eso",
					Controller: ptr.To(true),
					UID:        "uid-eso",
				}},
			},
		}
		reqs := r.enqueuePostgresDatabasesForExternalSecret(context.Background(), esoOwned)
		require.Len(t, reqs, 2, "ESO-owned external secret must enqueue every referencing PD")
		got := []string{reqs[0].Name, reqs[1].Name}
		sort.Strings(got)
		assert.Equal(t, []string{"pd-one", "pd-two"}, got)
	})

	t.Run("non-Secret object yields nil", func(t *testing.T) {
		reqs := r.enqueuePostgresDatabasesForExternalSecret(context.Background(), &corev1.ConfigMap{})
		assert.Nil(t, reqs)
	})
}
