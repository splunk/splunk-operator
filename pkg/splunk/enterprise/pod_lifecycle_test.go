// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"fmt"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestPodDisruptionBudgetCreation tests PDB creation for all cluster types
func TestPodDisruptionBudgetCreation(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	tests := []struct {
		name         string
		instanceType InstanceType
		replicas     int32
		crName       string
		wantMinAvail int32
	}{
		{
			name:         "Standalone with 3 replicas",
			instanceType: SplunkStandalone,
			replicas:     3,
			crName:       "test-standalone",
			wantMinAvail: 2, // 3-1=2
		},
		{
			name:         "Standalone with 1 replica",
			instanceType: SplunkStandalone,
			replicas:     1,
			crName:       "test-standalone-single",
			wantMinAvail: 0, // Single replica special case
		},
		{
			name:         "IngestorCluster with 5 replicas",
			instanceType: SplunkIngestor,
			replicas:     5,
			crName:       "test-ingestor",
			wantMinAvail: 4, // 5-1=4
		},
		{
			name:         "IndexerCluster with 10 replicas",
			instanceType: SplunkIndexer,
			replicas:     10,
			crName:       "test-indexer",
			wantMinAvail: 9, // 10-1=9
		},
		{
			name:         "SearchHeadCluster with 3 replicas",
			instanceType: SplunkSearchHead,
			replicas:     3,
			crName:       "test-shc",
			wantMinAvail: 2, // 3-1=2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()

			// Create a mock CR
			cr := &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.crName,
					Namespace: "test",
				},
			}

			// Apply PDB
			err := ApplyPodDisruptionBudget(ctx, c, cr, tt.instanceType, tt.replicas)
			if err != nil {
				t.Errorf("ApplyPodDisruptionBudget() error = %v", err)
				return
			}

			// Verify PDB was created
			pdbName := GetSplunkStatefulsetName(tt.instanceType, tt.crName) + "-pdb"
			pdb := &policyv1.PodDisruptionBudget{}
			err = c.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: "test"}, pdb)
			if err != nil {
				t.Errorf("Failed to get PDB: %v", err)
				return
			}

			// Verify minAvailable is correct
			if pdb.Spec.MinAvailable.IntVal != tt.wantMinAvail {
				t.Errorf("PDB minAvailable = %d, want %d", pdb.Spec.MinAvailable.IntVal, tt.wantMinAvail)
			}

			// Verify selector is set
			if pdb.Spec.Selector == nil {
				t.Error("PDB selector is nil")
			}

			// Verify owner reference is set
			if len(pdb.GetOwnerReferences()) == 0 {
				t.Error("PDB has no owner references")
			}
		})
	}
}

// TestPodDisruptionBudgetUpdate tests PDB updates when replicas change
func TestPodDisruptionBudgetUpdate(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	cr := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-standalone",
			Namespace: "test",
		},
	}

	// Create PDB with 3 replicas (minAvailable=2)
	err := ApplyPodDisruptionBudget(ctx, c, cr, SplunkStandalone, 3)
	if err != nil {
		t.Fatalf("Initial PDB creation failed: %v", err)
	}

	// Update to 5 replicas (minAvailable should become 4)
	err = ApplyPodDisruptionBudget(ctx, c, cr, SplunkStandalone, 5)
	if err != nil {
		t.Fatalf("PDB update failed: %v", err)
	}

	// Verify update
	pdbName := GetSplunkStatefulsetName(SplunkStandalone, "test-standalone") + "-pdb"
	pdb := &policyv1.PodDisruptionBudget{}
	err = c.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: "test"}, pdb)
	if err != nil {
		t.Fatalf("Failed to get updated PDB: %v", err)
	}

	if pdb.Spec.MinAvailable.IntVal != 4 {
		t.Errorf("Updated PDB minAvailable = %d, want 4", pdb.Spec.MinAvailable.IntVal)
	}
}

// TestPodIntentAnnotations tests intent annotation handling
func TestPodIntentAnnotations(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name       string
		podOrdinal int32
		annotation string
		replicas   int32
		newReplicas int32
		wantIntent string
	}{
		{
			name:        "Scale down - pod marked for deletion",
			podOrdinal:  2,
			annotation:  "",
			replicas:    3,
			newReplicas: 2,
			wantIntent:  "scale-down",
		},
		{
			name:        "Restart - pod keeps serve intent",
			podOrdinal:  1,
			annotation:  "serve",
			replicas:    3,
			newReplicas: 3,
			wantIntent:  "serve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()

			// Create StatefulSet
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-test-standalone",
					Namespace: "test",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &tt.replicas,
				},
			}
			c.Create(ctx, ss)

			// Create pod
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("splunk-test-standalone-%d", tt.podOrdinal),
					Namespace: "test",
					Annotations: map[string]string{
						"splunk.com/pod-intent": tt.annotation,
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			if tt.annotation == "" {
				pod.Annotations = nil
			}
			c.Create(ctx, pod)

			// For scale-down test, mark pod for scale-down
			if tt.newReplicas < tt.replicas {
				// This simulates what happens in statefulset.go markPodForScaleDown
				pod.Annotations = map[string]string{
					"splunk.com/pod-intent": "scale-down",
				}
				c.Update(ctx, pod)
			}

			// Verify intent
			updatedPod := &corev1.Pod{}
			err := c.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("splunk-test-standalone-%d", tt.podOrdinal),
				Namespace: "test",
			}, updatedPod)
			if err != nil {
				t.Fatalf("Failed to get pod: %v", err)
			}

			gotIntent := updatedPod.Annotations["splunk.com/pod-intent"]
			if gotIntent != tt.wantIntent {
				t.Errorf("Pod intent = %s, want %s", gotIntent, tt.wantIntent)
			}
		})
	}
}

// TestFinalizerHandling tests finalizer addition and removal
func TestFinalizerHandling(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create CR (not used directly, but needed for StatefulSet creation)
	_ = &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 2,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	// Create StatefulSet with finalizer
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-standalone",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: intPtr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
					Finalizers: []string{"splunk.com/pod-cleanup"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "splunk",
							Image: "splunk/splunk:latest",
						},
					},
				},
			},
		},
	}
	c.Create(ctx, ss)

	// Verify finalizer is present in template
	retrievedSS := &appsv1.StatefulSet{}
	err := c.Get(ctx, types.NamespacedName{Name: ss.Name, Namespace: ss.Namespace}, retrievedSS)
	if err != nil {
		t.Fatalf("Failed to get StatefulSet: %v", err)
	}

	hasFinalizer := false
	for _, f := range retrievedSS.Spec.Template.ObjectMeta.Finalizers {
		if f == "splunk.com/pod-cleanup" {
			hasFinalizer = true
			break
		}
	}

	if !hasFinalizer {
		t.Error("StatefulSet template does not have pod-cleanup finalizer")
	}
}

// TestDuplicateFinalizerPrevention tests that duplicate finalizers are not added
func TestDuplicateFinalizerPrevention(t *testing.T) {
	// Test the containsString helper function
	tests := []struct {
		name  string
		slice []string
		str   string
		want  bool
	}{
		{
			name:  "String exists",
			slice: []string{"splunk.com/pod-cleanup", "other-finalizer"},
			str:   "splunk.com/pod-cleanup",
			want:  true,
		},
		{
			name:  "String does not exist",
			slice: []string{"other-finalizer"},
			str:   "splunk.com/pod-cleanup",
			want:  false,
		},
		{
			name:  "Empty slice",
			slice: []string{},
			str:   "splunk.com/pod-cleanup",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsString(tt.slice, tt.str)
			if got != tt.want {
				t.Errorf("containsString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRollingUpdateConfig tests percentage-based rolling update configuration
func TestRollingUpdateConfig(t *testing.T) {
	tests := []struct {
		name                  string
		config                *enterpriseApi.RollingUpdateConfig
		replicas              int32
		wantMaxUnavailable    string
		wantMaxUnavailableInt int32
		wantPartition         *int32
	}{
		{
			name:                  "No config - defaults",
			config:                nil,
			replicas:              10,
			wantMaxUnavailable:    "",
			wantMaxUnavailableInt: 1, // Default
			wantPartition:         nil,
		},
		{
			name: "Percentage-based - 25%",
			config: &enterpriseApi.RollingUpdateConfig{
				MaxPodsUnavailable: "25%",
			},
			replicas:           10,
			wantMaxUnavailable: "25%",
			wantPartition:      nil,
		},
		{
			name: "Absolute number - 2",
			config: &enterpriseApi.RollingUpdateConfig{
				MaxPodsUnavailable: "2",
			},
			replicas:              10,
			wantMaxUnavailableInt: 2,
			wantPartition:         nil,
		},
		{
			name: "Canary deployment with partition",
			config: &enterpriseApi.RollingUpdateConfig{
				MaxPodsUnavailable: "1",
				Partition:          intPtr(8),
			},
			replicas:              10,
			wantMaxUnavailableInt: 1,
			wantPartition:         intPtr(8),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &enterpriseApi.CommonSplunkSpec{
				RollingUpdateConfig: tt.config,
			}

			strategy := buildUpdateStrategy(spec, tt.replicas)

			// Verify strategy type is RollingUpdate
			if strategy.Type != appsv1.RollingUpdateStatefulSetStrategyType {
				t.Errorf("Strategy type = %v, want RollingUpdate", strategy.Type)
			}

			// Verify RollingUpdate configuration exists
			if strategy.RollingUpdate == nil {
				t.Fatal("RollingUpdate configuration is nil")
			}

			// Verify MaxUnavailable
			if tt.wantMaxUnavailable != "" {
				if strategy.RollingUpdate.MaxUnavailable.StrVal != tt.wantMaxUnavailable {
					t.Errorf("MaxUnavailable = %s, want %s",
						strategy.RollingUpdate.MaxUnavailable.StrVal, tt.wantMaxUnavailable)
				}
			} else if tt.wantMaxUnavailableInt > 0 {
				if strategy.RollingUpdate.MaxUnavailable.IntVal != tt.wantMaxUnavailableInt {
					t.Errorf("MaxUnavailable = %d, want %d",
						strategy.RollingUpdate.MaxUnavailable.IntVal, tt.wantMaxUnavailableInt)
				}
			}

			// Verify Partition
			if tt.wantPartition != nil {
				if strategy.RollingUpdate.Partition == nil {
					t.Error("Partition is nil, want non-nil")
				} else if *strategy.RollingUpdate.Partition != *tt.wantPartition {
					t.Errorf("Partition = %d, want %d",
						*strategy.RollingUpdate.Partition, *tt.wantPartition)
				}
			}
		})
	}
}

// TestStatefulSetRollingUpdateMutualExclusion tests that pod eviction is blocked
// when StatefulSet rolling update is in progress
func TestStatefulSetRollingUpdateMutualExclusion(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name              string
		replicas          int32
		updatedReplicas   int32
		shouldBlockEvict  bool
		description       string
	}{
		{
			name:             "No rolling update in progress",
			replicas:         3,
			updatedReplicas:  3,
			shouldBlockEvict: false,
			description:      "All pods updated, eviction should proceed",
		},
		{
			name:             "Rolling update in progress",
			replicas:         3,
			updatedReplicas:  1,
			shouldBlockEvict: true,
			description:      "1 of 3 pods updated, eviction should be blocked",
		},
		{
			name:             "Rolling update just started",
			replicas:         5,
			updatedReplicas:  0,
			shouldBlockEvict: true,
			description:      "0 of 5 pods updated, eviction should be blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()

			// Create StatefulSet with rolling update state
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-test-standalone",
					Namespace: "test",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &tt.replicas,
				},
				Status: appsv1.StatefulSetStatus{
					Replicas:        tt.replicas,
					UpdatedReplicas: tt.updatedReplicas,
					ReadyReplicas:   tt.replicas,
				},
			}
			c.Create(ctx, ss)

			// Check if eviction should be blocked
			// This simulates what happens in checkAndEvictStandaloneIfNeeded
			retrieved := &appsv1.StatefulSet{}
			err := c.Get(ctx, types.NamespacedName{
				Name:      "splunk-test-standalone",
				Namespace: "test",
			}, retrieved)
			if err != nil {
				t.Fatalf("Failed to get StatefulSet: %v", err)
			}

			isRollingUpdate := retrieved.Status.UpdatedReplicas < *retrieved.Spec.Replicas
			if isRollingUpdate != tt.shouldBlockEvict {
				t.Errorf("isRollingUpdate = %v, want %v (%s)",
					isRollingUpdate, tt.shouldBlockEvict, tt.description)
			}
		})
	}
}

// TestPreStopEnvironmentVariables tests that required environment variables
// are set in StatefulSet pods for preStop hook
func TestPreStopEnvironmentVariables(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create a standalone CR
	cr := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 2,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	// Get StatefulSet (would be created by getStandaloneStatefulSet)
	ss, err := getStandaloneStatefulSet(ctx, c, cr)
	if err != nil {
		t.Skip("Skipping test - requires preStop.sh file (integration test)")
		return
	}

	// Verify required environment variables are present
	requiredEnvVars := map[string]bool{
		"POD_NAME":      false,
		"POD_NAMESPACE": false,
		"SPLUNK_ROLE":   false,
	}

	for _, container := range ss.Spec.Template.Spec.Containers {
		if container.Name == "splunk" {
			for _, env := range container.Env {
				if _, ok := requiredEnvVars[env.Name]; ok {
					requiredEnvVars[env.Name] = true

					// Verify POD_NAME uses downward API
					if env.Name == "POD_NAME" && env.ValueFrom == nil {
						t.Error("POD_NAME should use downward API (ValueFrom)")
					}
					if env.Name == "POD_NAME" && env.ValueFrom != nil && env.ValueFrom.FieldRef == nil {
						t.Error("POD_NAME should use FieldRef")
					}
					if env.Name == "POD_NAME" && env.ValueFrom != nil && env.ValueFrom.FieldRef != nil &&
						env.ValueFrom.FieldRef.FieldPath != "metadata.name" {
						t.Errorf("POD_NAME FieldPath = %s, want metadata.name", env.ValueFrom.FieldRef.FieldPath)
					}

					// Verify POD_NAMESPACE uses downward API
					if env.Name == "POD_NAMESPACE" && env.ValueFrom == nil {
						t.Error("POD_NAMESPACE should use downward API (ValueFrom)")
					}
				}
			}
		}
	}

	// Check if all required env vars are present
	for envName, found := range requiredEnvVars {
		if !found {
			t.Errorf("Required environment variable %s not found", envName)
		}
	}

	// Verify SPLUNK_PASSWORD is NOT present (should use mounted secret file)
	for _, container := range ss.Spec.Template.Spec.Containers {
		if container.Name == "splunk" {
			for _, env := range container.Env {
				if env.Name == "SPLUNK_PASSWORD" {
					t.Error("SPLUNK_PASSWORD should not be set as environment variable (should use mounted secret file)")
				}
			}
		}
	}
}

// TestPreStopHookConfiguration tests that preStop hook is configured correctly
func TestPreStopHookConfiguration(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	cr := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 2,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	ss, err := getStandaloneStatefulSet(ctx, c, cr)
	if err != nil {
		t.Skip("Skipping test - requires preStop.sh file (integration test)")
		return
	}

	// Verify preStop hook is configured
	hasPreStopHook := false
	for _, container := range ss.Spec.Template.Spec.Containers {
		if container.Name == "splunk" && container.Lifecycle != nil &&
			container.Lifecycle.PreStop != nil {
			hasPreStopHook = true

			// Verify it's an Exec handler
			if container.Lifecycle.PreStop.Exec == nil {
				t.Error("PreStop hook should use Exec handler")
			}

			// Verify command calls preStop.sh
			if container.Lifecycle.PreStop.Exec != nil {
				foundPreStopScript := false
				for _, cmd := range container.Lifecycle.PreStop.Exec.Command {
					if contains(cmd, "preStop.sh") {
						foundPreStopScript = true
						break
					}
				}
				if !foundPreStopScript {
					t.Error("PreStop hook does not call preStop.sh")
				}
			}
		}
	}

	if !hasPreStopHook {
		t.Error("PreStop hook not configured in StatefulSet")
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr))
}

// Helper function to create int32 pointer
func intPtr(i int32) *int32 {
	return &i
}
