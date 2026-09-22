// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package scssanity

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeSchemeForIngestorCluster(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register enterpriseApi scheme: %v", err)
	}
	return scheme
}

const (
	testIngestorName      = "example-tenant"
	testIngestorNamespace = "sok-example-tenant"
)

func TestDiscoverIngestorCluster_Found(t *testing.T) {
	scheme := newFakeSchemeForIngestorCluster(t)
	existing := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testIngestorName, Namespace: testIngestorNamespace},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	got, err := discoverIngestorCluster(context.Background(), kubeClient, testIngestorName, testIngestorNamespace, "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got.GetName() != testIngestorName || got.GetNamespace() != testIngestorNamespace {
		t.Errorf("got wrong object: %s/%s", got.GetNamespace(), got.GetName())
	}
}

func TestDiscoverIngestorCluster_NotFound(t *testing.T) {
	scheme := newFakeSchemeForIngestorCluster(t)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := discoverIngestorCluster(context.Background(), kubeClient, testIngestorName, testIngestorNamespace, "")
	if err == nil {
		t.Fatal("expected an error when the IngestorCluster does not exist, got nil")
	}
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected the wrapped error to satisfy apierrors.IsNotFound, got: %v", err)
	}
}

func TestDiscoverIngestorCluster_WrongNamespaceStillNotFound(t *testing.T) {
	scheme := newFakeSchemeForIngestorCluster(t)
	existing := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testIngestorName, Namespace: "sok-some-other-tenant"},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	_, err := discoverIngestorCluster(context.Background(), kubeClient, testIngestorName, testIngestorNamespace, "")
	if err == nil {
		t.Fatal("expected an error when the CR exists only in a different namespace, got nil")
	}
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected the wrapped error to satisfy apierrors.IsNotFound, got: %v", err)
	}
}
