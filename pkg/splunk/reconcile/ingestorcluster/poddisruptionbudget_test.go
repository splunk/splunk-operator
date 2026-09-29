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

package ingestorcluster

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestApplyPodDisruptionBudget(t *testing.T) {
	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	utilruntime.Must(policyv1.AddToScheme(sch))

	makeCR := func(replicas int32) *enterpriseApi.IngestorCluster {
		return &enterpriseApi.IngestorCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
				UID:       "owner-uid",
			},
			TypeMeta: metav1.TypeMeta{APIVersion: enterpriseApi.GroupVersion.String(), Kind: "IngestorCluster"},
			Spec: enterpriseApi.IngestorClusterSpec{
				Replicas: replicas,
			},
		}
	}

	pdbName := splutil.GetSplunkStatefulsetName(splcommon.SplunkIngestor, "test")

	// Create case: PDB does not exist yet
	t.Run("create", func(t *testing.T) {
		c := spltest.NewFakeClientBuilder(sch).Build()
		cr := makeCR(3)
		if err := applyPodDisruptionBudget(ctx, c, cr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var pdb policyv1.PodDisruptionBudget
		if err := c.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: "test"}, &pdb); err != nil {
			t.Fatalf("PDB not found after create: %v", err)
		}
		if pdb.Spec.MaxUnavailable == nil {
			t.Fatal("MaxUnavailable is nil")
		}
		if pdb.Spec.MaxUnavailable.IntValue() != 1 {
			t.Errorf("MaxUnavailable = %d; want 1", pdb.Spec.MaxUnavailable.IntValue())
		}
		wantInstance := "splunk-test-ingestor"
		if got := pdb.Spec.Selector.MatchLabels["app.kubernetes.io/instance"]; got != wantInstance {
			t.Errorf("spec selector instance = %q; want %q", got, wantInstance)
		}
		if got := pdb.Labels["app.kubernetes.io/managed-by"]; got != "splunk-operator" {
			t.Errorf("metadata label managed-by = %q; want splunk-operator", got)
		}
		if got := pdb.Labels["app.kubernetes.io/instance"]; got != wantInstance {
			t.Errorf("metadata label instance = %q; want %q", got, wantInstance)
		}
		if len(pdb.OwnerReferences) != 1 {
			t.Fatalf("owner references count = %d; want 1", len(pdb.OwnerReferences))
		}
		if got := pdb.OwnerReferences[0].APIVersion; got != enterpriseApi.GroupVersion.String() {
			t.Errorf("owner reference APIVersion = %q; want %q", got, enterpriseApi.GroupVersion.String())
		}
		if got := pdb.OwnerReferences[0].Kind; got != "IngestorCluster" {
			t.Errorf("owner reference Kind = %q; want IngestorCluster", got)
		}
		if got := pdb.OwnerReferences[0].UID; got != cr.UID {
			t.Errorf("owner reference UID = %q; want %q", got, cr.UID)
		}
	})

	// Conflict case: PDB with the expected name exists but is owned by a different CR
	t.Run("error-when-owned-by-other-cr", func(t *testing.T) {
		controller := true
		foreign := &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pdbName,
				Namespace: "test",
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "splunk-operator",
					"app.kubernetes.io/instance":   "splunk-test-ingestor",
				},
				OwnerReferences: []metav1.OwnerReference{
					{UID: "some-other-uid", Controller: &controller},
				},
			},
		}
		c := spltest.NewFakeClientBuilder(sch).WithObjects(foreign).Build()
		cr := makeCR(3)
		if err := applyPodDisruptionBudget(ctx, c, cr); err == nil {
			t.Fatal("expected error for PDB owned by different CR, got nil")
		}
	})

	// Existing owned case: PDB already exists and is owned by this CR.
	t.Run("no-op-when-owned-by-this-cr", func(t *testing.T) {
		c := spltest.NewFakeClientBuilder(sch).Build()
		cr := makeCR(3)

		// First call creates the PDB
		if err := applyPodDisruptionBudget(ctx, c, cr); err != nil {
			t.Fatalf("unexpected error on create: %v", err)
		}

		// Second call (different replica count) must still be accepted for the same owner.
		cr2 := makeCR(5)
		if err := applyPodDisruptionBudget(ctx, c, cr2); err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}

		// MaxUnavailable must still be 1
		var pdb policyv1.PodDisruptionBudget
		if err := c.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: "test"}, &pdb); err != nil {
			t.Fatalf("PDB not found: %v", err)
		}
		if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.IntValue() != 1 {
			t.Errorf("MaxUnavailable changed after scale; want 1")
		}
	})
}
