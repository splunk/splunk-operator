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

package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAsOwner(t *testing.T) {
	cr := TestResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	boolValues := [2]bool{true, false}

	for b := range boolValues {
		got := AsOwner(&cr, boolValues[b])

		if got.APIVersion != cr.TypeMeta.APIVersion {
			t.Errorf("AsOwner().APIVersion = %s; want %s", got.APIVersion, cr.TypeMeta.APIVersion)
		}

		if got.Kind != cr.TypeMeta.Kind {
			t.Errorf("AsOwner().Kind = %s; want %s", got.Kind, cr.TypeMeta.Kind)
		}

		if got.Name != cr.Name {
			t.Errorf("AsOwner().Name = %s; want %s", got.Name, cr.Name)
		}

		if got.UID != cr.UID {
			t.Errorf("AsOwner().UID = %s; want %s", got.UID, cr.UID)
		}

		if *got.Controller != boolValues[b] {
			t.Errorf("AsOwner().Controller = %t; want %t", *got.Controller, true)
		}
	}
}

func TestAppendParentMeta(t *testing.T) {
	parent := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"a": "b",
			},
			Annotations: map[string]string{
				"one": "two",
				"kubectl.kubernetes.io/last-applied-configuration": "foobar",
			},
		},
	}
	child := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"a": "z",
				"c": "d",
			},
			Annotations: map[string]string{
				"three": "four",
				"one":   "ten",
			},
		},
	}

	AppendParentMeta(child.GetObjectMeta(), parent.GetObjectMeta())

	// check Labels
	want := map[string]string{
		"a": "z",
		"c": "d",
	}
	if !reflect.DeepEqual(child.GetLabels(), want) {
		t.Errorf("AppendParentMeta() child Labels=%v; want %v", child.GetLabels(), want)
	}

	// check Annotations
	want = map[string]string{
		"one":   "ten",
		"three": "four",
	}
	if !reflect.DeepEqual(child.GetAnnotations(), want) {
		t.Errorf("AppendParentMeta() child Annotations=%v; want %v", child.GetAnnotations(), want)
	}

	parent.Labels["abc"] = "def"
	parent.Annotations["abc"] = "def"
	AppendParentMeta(child.GetObjectMeta(), parent.GetObjectMeta())

}

func TestHasExcludedPrefix(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		prefixes []string
		expected bool
	}{
		{
			name:     "sts-only prefix excluded from pod template",
			key:      "sts-only.amadeus.com/priority",
			prefixes: podTemplateExcludedPrefixes,
			expected: true,
		},
		{
			name:     "pod-only prefix not excluded from pod template",
			key:      "pod-only.prometheus.io/scrape",
			prefixes: podTemplateExcludedPrefixes,
			expected: false,
		},
		{
			name:     "regular label not excluded from pod template",
			key:      "team",
			prefixes: podTemplateExcludedPrefixes,
			expected: false,
		},
		{
			name:     "pod-only prefix excluded from statefulset",
			key:      "pod-only.prometheus.io/scrape",
			prefixes: statefulSetExcludedPrefixes,
			expected: true,
		},
		{
			name:     "sts-only prefix not excluded from statefulset",
			key:      "sts-only.amadeus.com/priority",
			prefixes: statefulSetExcludedPrefixes,
			expected: false,
		},
		{
			name:     "kubectl prefix excluded from both",
			key:      "kubectl.kubernetes.io/last-applied",
			prefixes: podTemplateExcludedPrefixes,
			expected: true,
		},
		{
			name:     "operator prefix excluded from both",
			key:      "operator.splunk.com/internal",
			prefixes: statefulSetExcludedPrefixes,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasExcludedPrefix(tt.key, tt.prefixes)
			if got != tt.expected {
				t.Errorf("hasExcludedPrefix(%q, %v) = %v; want %v", tt.key, tt.prefixes, got, tt.expected)
			}
		})
	}
}

func TestManagedCRAnnotationConstants(t *testing.T) {
	// Verify constants have expected values
	if ManagedCRLabelKeysAnnotation != "operator.splunk.com/managed-cr-label-keys" {
		t.Errorf("ManagedCRLabelKeysAnnotation = %q; want %q", ManagedCRLabelKeysAnnotation, "operator.splunk.com/managed-cr-label-keys")
	}
	if ManagedCRAnnotationKeysAnnotation != "operator.splunk.com/managed-cr-annotation-keys" {
		t.Errorf("ManagedCRAnnotationKeysAnnotation = %q; want %q", ManagedCRAnnotationKeysAnnotation, "operator.splunk.com/managed-cr-annotation-keys")
	}
}

func TestGetManagedLabelKeys(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    []string
	}{
		{
			name:        "nil annotations returns empty slice",
			annotations: nil,
			expected:    []string{},
		},
		{
			name:        "empty annotations returns empty slice",
			annotations: map[string]string{},
			expected:    []string{},
		},
		{
			name: "missing annotation returns empty slice",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			expected: []string{},
		},
		{
			name: "empty annotation value returns empty slice",
			annotations: map[string]string{
				ManagedCRLabelKeysAnnotation: "",
			},
			expected: []string{},
		},
		{
			name: "invalid JSON returns empty slice",
			annotations: map[string]string{
				ManagedCRLabelKeysAnnotation: "not-valid-json",
			},
			expected: []string{},
		},
		{
			name: "empty JSON array returns empty slice",
			annotations: map[string]string{
				ManagedCRLabelKeysAnnotation: "[]",
			},
			expected: []string{},
		},
		{
			name: "single key",
			annotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["team"]`,
			},
			expected: []string{"team"},
		},
		{
			name: "multiple keys",
			annotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["environment","team","version"]`,
			},
			expected: []string{"environment", "team", "version"},
		},
		{
			name: "keys with special characters",
			annotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["mycompany.com/cost-center","app.kubernetes.io/name"]`,
			},
			expected: []string{"mycompany.com/cost-center", "app.kubernetes.io/name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetManagedLabelKeys(tt.annotations)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("GetManagedLabelKeys() = %v; want %v", got, tt.expected)
			}
		})
	}
}

func TestGetManagedAnnotationKeys(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    []string
	}{
		{
			name:        "nil annotations returns empty slice",
			annotations: nil,
			expected:    []string{},
		},
		{
			name:        "empty annotations returns empty slice",
			annotations: map[string]string{},
			expected:    []string{},
		},
		{
			name: "missing annotation returns empty slice",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			expected: []string{},
		},
		{
			name: "empty annotation value returns empty slice",
			annotations: map[string]string{
				ManagedCRAnnotationKeysAnnotation: "",
			},
			expected: []string{},
		},
		{
			name: "invalid JSON returns empty slice",
			annotations: map[string]string{
				ManagedCRAnnotationKeysAnnotation: "{invalid}",
			},
			expected: []string{},
		},
		{
			name: "empty JSON array returns empty slice",
			annotations: map[string]string{
				ManagedCRAnnotationKeysAnnotation: "[]",
			},
			expected: []string{},
		},
		{
			name: "single key",
			annotations: map[string]string{
				ManagedCRAnnotationKeysAnnotation: `["description"]`,
			},
			expected: []string{"description"},
		},
		{
			name: "multiple keys",
			annotations: map[string]string{
				ManagedCRAnnotationKeysAnnotation: `["contact","description","owner"]`,
			},
			expected: []string{"contact", "description", "owner"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetManagedAnnotationKeys(tt.annotations)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("GetManagedAnnotationKeys() = %v; want %v", got, tt.expected)
			}
		})
	}
}

func TestSetManagedLabelKeys(t *testing.T) {
	tests := []struct {
		name               string
		annotations        map[string]string
		keys               []string
		expectedAnnotation string
		shouldExist        bool
	}{
		{
			name:        "nil annotations is no-op",
			annotations: nil,
			keys:        []string{"team"},
			shouldExist: false,
		},
		{
			name:        "nil keys removes annotation",
			annotations: map[string]string{ManagedCRLabelKeysAnnotation: `["old"]`},
			keys:        nil,
			shouldExist: false,
		},
		{
			name:        "empty keys removes annotation",
			annotations: map[string]string{ManagedCRLabelKeysAnnotation: `["old"]`},
			keys:        []string{},
			shouldExist: false,
		},
		{
			name:               "single key",
			annotations:        map[string]string{},
			keys:               []string{"team"},
			expectedAnnotation: `["team"]`,
			shouldExist:        true,
		},
		{
			name:               "multiple keys are sorted",
			annotations:        map[string]string{},
			keys:               []string{"zebra", "alpha", "middle"},
			expectedAnnotation: `["alpha","middle","zebra"]`,
			shouldExist:        true,
		},
		{
			name:               "keys with special characters",
			annotations:        map[string]string{},
			keys:               []string{"mycompany.com/team", "app.kubernetes.io/name"},
			expectedAnnotation: `["app.kubernetes.io/name","mycompany.com/team"]`,
			shouldExist:        true,
		},
		{
			name:               "overwrites existing annotation",
			annotations:        map[string]string{ManagedCRLabelKeysAnnotation: `["old"]`},
			keys:               []string{"new"},
			expectedAnnotation: `["new"]`,
			shouldExist:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetManagedLabelKeys(tt.annotations, tt.keys)
			if tt.annotations == nil {
				return // no-op case
			}
			got, exists := tt.annotations[ManagedCRLabelKeysAnnotation]
			if exists != tt.shouldExist {
				t.Errorf("SetManagedLabelKeys() annotation exists = %v; want %v", exists, tt.shouldExist)
			}
			if tt.shouldExist && got != tt.expectedAnnotation {
				t.Errorf("SetManagedLabelKeys() annotation = %q; want %q", got, tt.expectedAnnotation)
			}
		})
	}
}

func TestSetManagedAnnotationKeys(t *testing.T) {
	tests := []struct {
		name               string
		annotations        map[string]string
		keys               []string
		expectedAnnotation string
		shouldExist        bool
	}{
		{
			name:        "nil annotations is no-op",
			annotations: nil,
			keys:        []string{"description"},
			shouldExist: false,
		},
		{
			name:        "nil keys removes annotation",
			annotations: map[string]string{ManagedCRAnnotationKeysAnnotation: `["old"]`},
			keys:        nil,
			shouldExist: false,
		},
		{
			name:        "empty keys removes annotation",
			annotations: map[string]string{ManagedCRAnnotationKeysAnnotation: `["old"]`},
			keys:        []string{},
			shouldExist: false,
		},
		{
			name:               "single key",
			annotations:        map[string]string{},
			keys:               []string{"description"},
			expectedAnnotation: `["description"]`,
			shouldExist:        true,
		},
		{
			name:               "multiple keys are sorted",
			annotations:        map[string]string{},
			keys:               []string{"owner", "contact", "description"},
			expectedAnnotation: `["contact","description","owner"]`,
			shouldExist:        true,
		},
		{
			name:               "overwrites existing annotation",
			annotations:        map[string]string{ManagedCRAnnotationKeysAnnotation: `["old"]`},
			keys:               []string{"new"},
			expectedAnnotation: `["new"]`,
			shouldExist:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetManagedAnnotationKeys(tt.annotations, tt.keys)
			if tt.annotations == nil {
				return // no-op case
			}
			got, exists := tt.annotations[ManagedCRAnnotationKeysAnnotation]
			if exists != tt.shouldExist {
				t.Errorf("SetManagedAnnotationKeys() annotation exists = %v; want %v", exists, tt.shouldExist)
			}
			if tt.shouldExist && got != tt.expectedAnnotation {
				t.Errorf("SetManagedAnnotationKeys() annotation = %q; want %q", got, tt.expectedAnnotation)
			}
		})
	}
}

func TestManagedKeysRoundTrip(t *testing.T) {
	// Test that Set followed by Get returns the same keys (sorted)
	tests := []struct {
		name     string
		keys     []string
		expected []string
	}{
		{
			name:     "empty keys",
			keys:     []string{},
			expected: []string{},
		},
		{
			name:     "single key",
			keys:     []string{"team"},
			expected: []string{"team"},
		},
		{
			name:     "multiple keys unsorted",
			keys:     []string{"zebra", "alpha", "middle"},
			expected: []string{"alpha", "middle", "zebra"},
		},
		{
			name:     "keys with special characters",
			keys:     []string{"mycompany.com/team", "app.kubernetes.io/name", "environment"},
			expected: []string{"app.kubernetes.io/name", "environment", "mycompany.com/team"},
		},
	}

	for _, tt := range tests {
		t.Run("labels: "+tt.name, func(t *testing.T) {
			annotations := map[string]string{}
			SetManagedLabelKeys(annotations, tt.keys)
			got := GetManagedLabelKeys(annotations)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Round trip labels: got %v; want %v", got, tt.expected)
			}
		})

		t.Run("annotations: "+tt.name, func(t *testing.T) {
			annotations := map[string]string{}
			SetManagedAnnotationKeys(annotations, tt.keys)
			got := GetManagedAnnotationKeys(annotations)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Round trip annotations: got %v; want %v", got, tt.expected)
			}
		})
	}
}

func TestIsManagedKey(t *testing.T) {
	tests := []struct {
		name             string
		key              string
		excludedPrefixes []string
		expected         bool
	}{
		{
			name:             "user label is managed",
			key:              "team",
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "user label with domain is managed",
			key:              "mycompany.com/team",
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "kubectl prefix is not managed",
			key:              "kubectl.kubernetes.io/last-applied-configuration",
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "operator prefix is not managed",
			key:              "operator.splunk.com/internal",
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "sts-only prefix is not managed for pod template",
			key:              "sts-only.amadeus.com/priority",
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "pod-only prefix is managed for pod template",
			key:              "pod-only.prometheus.io/scrape",
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "pod-only prefix is not managed for statefulset",
			key:              "pod-only.prometheus.io/scrape",
			excludedPrefixes: statefulSetExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "sts-only prefix is managed for statefulset",
			key:              "sts-only.amadeus.com/priority",
			excludedPrefixes: statefulSetExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "app.kubernetes.io labels are managed",
			key:              "app.kubernetes.io/name",
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "empty prefixes means all keys are managed",
			key:              "operator.splunk.com/internal",
			excludedPrefixes: []string{},
			expected:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsManagedKey(tt.key, tt.excludedPrefixes)
			if got != tt.expected {
				t.Errorf("IsManagedKey(%q, %v) = %v; want %v", tt.key, tt.excludedPrefixes, got, tt.expected)
			}
		})
	}
}

func TestIsProtectedKey(t *testing.T) {
	selectorLabels := map[string]string{
		"app.kubernetes.io/name":       "indexer",
		"app.kubernetes.io/instance":   "splunk-test-indexer",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/part-of":    "splunk-test-indexer",
	}

	tests := []struct {
		name             string
		key              string
		selectorLabels   map[string]string
		excludedPrefixes []string
		expected         bool
	}{
		{
			name:             "selector label is protected",
			key:              "app.kubernetes.io/name",
			selectorLabels:   selectorLabels,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "selector label instance is protected",
			key:              "app.kubernetes.io/instance",
			selectorLabels:   selectorLabels,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "operator prefix is protected",
			key:              "operator.splunk.com/internal",
			selectorLabels:   selectorLabels,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "kubectl prefix is protected",
			key:              "kubectl.kubernetes.io/last-applied-configuration",
			selectorLabels:   selectorLabels,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "user label is not protected",
			key:              "team",
			selectorLabels:   selectorLabels,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "user label with domain is not protected",
			key:              "mycompany.com/cost-center",
			selectorLabels:   selectorLabels,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "empty selector labels - excluded prefix still protected",
			key:              "operator.splunk.com/internal",
			selectorLabels:   map[string]string{},
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "empty selector labels - user key not protected",
			key:              "team",
			selectorLabels:   map[string]string{},
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "nil selector labels - excluded prefix still protected",
			key:              "kubectl.kubernetes.io/last-applied",
			selectorLabels:   nil,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "nil selector labels - user key not protected",
			key:              "environment",
			selectorLabels:   nil,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
		{
			name:             "sts-only prefix protected for pod template",
			key:              "sts-only.amadeus.com/priority",
			selectorLabels:   selectorLabels,
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         true,
		},
		{
			name:             "pod-only prefix not protected for pod template",
			key:              "pod-only.prometheus.io/scrape",
			selectorLabels:   map[string]string{},
			excludedPrefixes: podTemplateExcludedPrefixes,
			expected:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsProtectedKey(tt.key, tt.selectorLabels, tt.excludedPrefixes)
			if got != tt.expected {
				t.Errorf("IsProtectedKey(%q, %v, %v) = %v; want %v", tt.key, tt.selectorLabels, tt.excludedPrefixes, got, tt.expected)
			}
		})
	}
}

func TestStripTargetPrefix(t *testing.T) {
	tests := []struct {
		name            string
		key             string
		prefix          string
		wantKey         string
		wantTransformed bool
	}{
		{
			name:            "strips pod-only prefix with prometheus.io domain",
			key:             "pod-only.prometheus.io/scrape",
			prefix:          "pod-only.",
			wantKey:         "prometheus.io/scrape",
			wantTransformed: true,
		},
		{
			name:            "strips pod-only prefix with istio.io domain",
			key:             "pod-only.istio.io/inject",
			prefix:          "pod-only.",
			wantKey:         "istio.io/inject",
			wantTransformed: true,
		},
		{
			name:            "strips sts-only prefix with amadeus.com domain",
			key:             "sts-only.amadeus.com/priority",
			prefix:          "sts-only.",
			wantKey:         "amadeus.com/priority",
			wantTransformed: true,
		},
		{
			name:            "strips sts-only prefix with custom domain",
			key:             "sts-only.custom.example.org/metric",
			prefix:          "sts-only.",
			wantKey:         "custom.example.org/metric",
			wantTransformed: true,
		},
		{
			name:            "no-op for non-matching prefix",
			key:             "team",
			prefix:          "pod-only.",
			wantKey:         "team",
			wantTransformed: false,
		},
		{
			name:            "no-op for different prefix",
			key:             "sts-only.amadeus.com/priority",
			prefix:          "pod-only.",
			wantKey:         "sts-only.amadeus.com/priority",
			wantTransformed: false,
		},
		{
			name:            "strips prefix leaving empty remainder",
			key:             "pod-only.",
			prefix:          "pod-only.",
			wantKey:         "",
			wantTransformed: true,
		},
		{
			name:            "empty key",
			key:             "",
			prefix:          "pod-only.",
			wantKey:         "",
			wantTransformed: false,
		},
		{
			name:            "partial prefix match should not transform",
			key:             "pod-only",
			prefix:          "pod-only.",
			wantKey:         "pod-only",
			wantTransformed: false,
		},
		{
			name:            "preserves multiple slashes in key",
			key:             "pod-only.company.com/category/subcategory",
			prefix:          "pod-only.",
			wantKey:         "company.com/category/subcategory",
			wantTransformed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotTransformed := stripTargetPrefix(tt.key, tt.prefix)
			if gotKey != tt.wantKey {
				t.Errorf("stripTargetPrefix() key = %q; want %q", gotKey, tt.wantKey)
			}
			if gotTransformed != tt.wantTransformed {
				t.Errorf("stripTargetPrefix() transformed = %v; want %v", gotTransformed, tt.wantTransformed)
			}
		})
	}
}

func TestAppendParentMeta_PrefixFiltering(t *testing.T) {
	parent := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"team":                          "platform",
				"sts-only.amadeus.com/priority": "high", // Should be filtered (StatefulSet-only)
				"pod-only.prometheus.io/scrape": "true", // Should be TRANSFORMED to prometheus.io/scrape
			},
			Annotations: map[string]string{
				"description":                "test",
				"sts-only.example.com/owner": "ops-team", // Should be filtered
				"pod-only.istio.io/inject":   "true",     // Should be TRANSFORMED to istio.io/inject
			},
		},
	}
	child := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
	}

	AppendParentMeta(child.GetObjectMeta(), parent.GetObjectMeta())

	// Verify labels
	if child.Labels["team"] != "platform" {
		t.Errorf("Expected label 'team' to be 'platform', got %q", child.Labels["team"])
	}
	// pod-only.prometheus.io/scrape should be TRANSFORMED to prometheus.io/scrape
	if child.Labels["prometheus.io/scrape"] != "true" {
		t.Errorf("Expected label 'prometheus.io/scrape' to be 'true', got %q", child.Labels["prometheus.io/scrape"])
	}
	// Original key should NOT exist
	if _, exists := child.Labels["pod-only.prometheus.io/scrape"]; exists {
		t.Errorf("Label 'pod-only.prometheus.io/scrape' should have been transformed, not copied as-is")
	}
	if _, exists := child.Labels["sts-only.amadeus.com/priority"]; exists {
		t.Errorf("Label 'sts-only.amadeus.com/priority' should have been filtered out")
	}

	// Verify annotations
	if child.Annotations["description"] != "test" {
		t.Errorf("Expected annotation 'description' to be 'test', got %q", child.Annotations["description"])
	}
	// pod-only.istio.io/inject should be TRANSFORMED to istio.io/inject
	if child.Annotations["istio.io/inject"] != "true" {
		t.Errorf("Expected annotation 'istio.io/inject' to be 'true', got %q", child.Annotations["istio.io/inject"])
	}
	// Original key should NOT exist
	if _, exists := child.Annotations["pod-only.istio.io/inject"]; exists {
		t.Errorf("Annotation 'pod-only.istio.io/inject' should have been transformed, not copied as-is")
	}
	if _, exists := child.Annotations["sts-only.example.com/owner"]; exists {
		t.Errorf("Annotation 'sts-only.example.com/owner' should have been filtered out")
	}
}

func TestComputeDesiredPodTemplateKeys(t *testing.T) {
	tests := []struct {
		name                string
		parentLabels        map[string]string
		parentAnnotations   map[string]string
		expectedLabels      map[string]string
		expectedAnnotations map[string]string
	}{
		{
			name:                "nil parent metadata",
			parentLabels:        nil,
			parentAnnotations:   nil,
			expectedLabels:      map[string]string{},
			expectedAnnotations: map[string]string{},
		},
		{
			name:                "empty parent metadata",
			parentLabels:        map[string]string{},
			parentAnnotations:   map[string]string{},
			expectedLabels:      map[string]string{},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "regular user labels and annotations pass through",
			parentLabels: map[string]string{
				"team":        "platform",
				"environment": "production",
			},
			parentAnnotations: map[string]string{
				"description": "test service",
				"owner":       "ops-team",
			},
			expectedLabels: map[string]string{
				"team":        "platform",
				"environment": "production",
			},
			expectedAnnotations: map[string]string{
				"description": "test service",
				"owner":       "ops-team",
			},
		},
		{
			name: "kubectl prefix is excluded",
			parentLabels: map[string]string{
				"team": "platform",
				"kubectl.kubernetes.io/last-applied-configuration": "json-data",
			},
			parentAnnotations: map[string]string{
				"description":                       "test",
				"kubectl.kubernetes.io/restartedAt": "2024-01-01",
			},
			expectedLabels: map[string]string{
				"team": "platform",
			},
			expectedAnnotations: map[string]string{
				"description": "test",
			},
		},
		{
			name: "operator prefix is excluded",
			parentLabels: map[string]string{
				"team":                       "platform",
				"operator.splunk.com/status": "active",
			},
			parentAnnotations: map[string]string{
				"description":                  "test",
				"operator.splunk.com/internal": "data",
			},
			expectedLabels: map[string]string{
				"team": "platform",
			},
			expectedAnnotations: map[string]string{
				"description": "test",
			},
		},
		{
			name: "sts-only prefix is excluded for pod template",
			parentLabels: map[string]string{
				"team":                          "platform",
				"sts-only.amadeus.com/priority": "high",
			},
			parentAnnotations: map[string]string{
				"description":                "test",
				"sts-only.example.com/owner": "ops-team",
			},
			expectedLabels: map[string]string{
				"team": "platform",
			},
			expectedAnnotations: map[string]string{
				"description": "test",
			},
		},
		{
			name: "pod-only prefix is transformed by stripping prefix",
			parentLabels: map[string]string{
				"team":                          "platform",
				"pod-only.prometheus.io/scrape": "true",
			},
			parentAnnotations: map[string]string{
				"description":              "test",
				"pod-only.istio.io/inject": "true",
			},
			expectedLabels: map[string]string{
				"team":                 "platform",
				"prometheus.io/scrape": "true",
			},
			expectedAnnotations: map[string]string{
				"description":     "test",
				"istio.io/inject": "true",
			},
		},
		{
			name: "mixed scenario with all prefix types",
			parentLabels: map[string]string{
				"team":                               "platform",
				"kubectl.kubernetes.io/last-applied": "config",
				"operator.splunk.com/internal":       "data",
				"sts-only.amadeus.com/priority":      "high",
				"pod-only.prometheus.io/scrape":      "true",
				"mycompany.com/cost-center":          "67890",
			},
			parentAnnotations: map[string]string{
				"description":                       "test",
				"kubectl.kubernetes.io/restartedAt": "2024-01-01",
				"operator.splunk.com/managed":       "true",
				"sts-only.example.com/owner":        "ops",
				"pod-only.istio.io/inject":          "true",
				"mycompany.com/owner":               "team-a",
			},
			expectedLabels: map[string]string{
				"team":                      "platform",
				"prometheus.io/scrape":      "true",
				"mycompany.com/cost-center": "67890",
			},
			expectedAnnotations: map[string]string{
				"description":         "test",
				"istio.io/inject":     "true",
				"mycompany.com/owner": "team-a",
			},
		},
		{
			name: "app.kubernetes.io labels pass through",
			parentLabels: map[string]string{
				"app.kubernetes.io/name":    "my-app",
				"app.kubernetes.io/version": "1.0.0",
			},
			parentAnnotations: map[string]string{},
			expectedLabels: map[string]string{
				"app.kubernetes.io/name":    "my-app",
				"app.kubernetes.io/version": "1.0.0",
			},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "conflict resolution: prefixed key wins over explicit key (more specific)",
			parentLabels: map[string]string{
				"prometheus.io/scrape":          "false", // explicit
				"pod-only.prometheus.io/scrape": "true",  // would transform to same key
			},
			parentAnnotations: map[string]string{
				"istio.io/inject":          "false", // explicit
				"pod-only.istio.io/inject": "true",  // would transform to same key
			},
			expectedLabels: map[string]string{
				"prometheus.io/scrape": "true", // prefixed wins (more specific)
			},
			expectedAnnotations: map[string]string{
				"istio.io/inject": "true", // prefixed wins (more specific)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      tt.parentLabels,
					Annotations: tt.parentAnnotations,
				},
			}

			gotLabels, gotAnnotations := ComputeDesiredPodTemplateKeys(parent.GetObjectMeta())

			if !reflect.DeepEqual(gotLabels, tt.expectedLabels) {
				t.Errorf("ComputeDesiredPodTemplateKeys() labels = %v; want %v", gotLabels, tt.expectedLabels)
			}
			if !reflect.DeepEqual(gotAnnotations, tt.expectedAnnotations) {
				t.Errorf("ComputeDesiredPodTemplateKeys() annotations = %v; want %v", gotAnnotations, tt.expectedAnnotations)
			}
		})
	}
}

func TestSyncParentMetaToPodTemplate(t *testing.T) {
	tests := []struct {
		name                       string
		childLabels                map[string]string
		childAnnotations           map[string]string
		parentLabels               map[string]string
		parentAnnotations          map[string]string
		previousManagedLabels      []string
		previousManagedAnnotations []string
		expectedChildLabels        map[string]string
		expectedChildAnnotations   map[string]string
		expectedManagedLabels      []string
		expectedManagedAnnotations []string
	}{
		{
			name:                       "add new labels and annotations to empty child",
			childLabels:                map[string]string{},
			childAnnotations:           map[string]string{},
			parentLabels:               map[string]string{"team": "platform", "environment": "prod"},
			parentAnnotations:          map[string]string{"description": "test"},
			previousManagedLabels:      []string{},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{"team": "platform", "environment": "prod"},
			expectedChildAnnotations:   map[string]string{"description": "test"},
			expectedManagedLabels:      []string{"environment", "team"},
			expectedManagedAnnotations: []string{"description"},
		},
		{
			name:                       "add new labels to child with nil maps",
			childLabels:                nil,
			childAnnotations:           nil,
			parentLabels:               map[string]string{"team": "platform"},
			parentAnnotations:          map[string]string{"owner": "ops"},
			previousManagedLabels:      []string{},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{"team": "platform"},
			expectedChildAnnotations:   map[string]string{"owner": "ops"},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{"owner"},
		},
		{
			name:                       "update existing managed labels",
			childLabels:                map[string]string{"team": "old-team", "existing": "keep"},
			childAnnotations:           map[string]string{"description": "old-desc"},
			parentLabels:               map[string]string{"team": "new-team"},
			parentAnnotations:          map[string]string{"description": "new-desc"},
			previousManagedLabels:      []string{"team"},
			previousManagedAnnotations: []string{"description"},
			expectedChildLabels:        map[string]string{"team": "new-team", "existing": "keep"},
			expectedChildAnnotations:   map[string]string{"description": "new-desc"},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{"description"},
		},
		{
			name:                       "remove previously managed label no longer in parent",
			childLabels:                map[string]string{"team": "platform", "old-key": "to-remove", "external": "keep"},
			childAnnotations:           map[string]string{"description": "test", "old-annotation": "to-remove"},
			parentLabels:               map[string]string{"team": "platform"},
			parentAnnotations:          map[string]string{"description": "test"},
			previousManagedLabels:      []string{"team", "old-key"},
			previousManagedAnnotations: []string{"description", "old-annotation"},
			expectedChildLabels:        map[string]string{"team": "platform", "external": "keep"},
			expectedChildAnnotations:   map[string]string{"description": "test"},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{"description"},
		},
		{
			name:                       "preserve external labels not in previousManaged",
			childLabels:                map[string]string{"team": "platform", "external-label": "keep-me"},
			childAnnotations:           map[string]string{"external-annotation": "also-keep"},
			parentLabels:               map[string]string{"team": "new-team"},
			parentAnnotations:          map[string]string{},
			previousManagedLabels:      []string{"team"},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{"team": "new-team", "external-label": "keep-me"},
			expectedChildAnnotations:   map[string]string{"external-annotation": "also-keep"},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{},
		},
		{
			name:                       "pod-only prefix transformation during sync",
			childLabels:                map[string]string{},
			childAnnotations:           map[string]string{},
			parentLabels:               map[string]string{"pod-only.prometheus.io/scrape": "true"},
			parentAnnotations:          map[string]string{"pod-only.istio.io/inject": "true"},
			previousManagedLabels:      []string{},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{"prometheus.io/scrape": "true"},
			expectedChildAnnotations:   map[string]string{"istio.io/inject": "true"},
			expectedManagedLabels:      []string{"prometheus.io/scrape"},
			expectedManagedAnnotations: []string{"istio.io/inject"},
		},
		{
			name:             "excluded prefixes are not synced",
			childLabels:      map[string]string{},
			childAnnotations: map[string]string{},
			parentLabels: map[string]string{
				"team":                               "platform",
				"kubectl.kubernetes.io/last-applied": "config",
				"operator.splunk.com/internal":       "data",
				"sts-only.amadeus.com/priority":      "high",
			},
			parentAnnotations: map[string]string{
				"description":                       "test",
				"kubectl.kubernetes.io/restartedAt": "2024-01-01",
			},
			previousManagedLabels:      []string{},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{"team": "platform"},
			expectedChildAnnotations:   map[string]string{"description": "test"},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{"description"},
		},
		{
			name:                       "remove all managed keys when parent has none",
			childLabels:                map[string]string{"team": "platform", "env": "prod", "external": "keep"},
			childAnnotations:           map[string]string{"description": "test", "owner": "ops"},
			parentLabels:               map[string]string{},
			parentAnnotations:          map[string]string{},
			previousManagedLabels:      []string{"team", "env"},
			previousManagedAnnotations: []string{"description", "owner"},
			expectedChildLabels:        map[string]string{"external": "keep"},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{},
			expectedManagedAnnotations: []string{},
		},
		{
			name:                       "add update and remove in single sync",
			childLabels:                map[string]string{"keep": "v1", "update": "old", "remove": "gone"},
			childAnnotations:           map[string]string{},
			parentLabels:               map[string]string{"keep": "v1", "update": "new", "add": "fresh"},
			parentAnnotations:          map[string]string{},
			previousManagedLabels:      []string{"keep", "update", "remove"},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{"keep": "v1", "update": "new", "add": "fresh"},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{"add", "keep", "update"},
			expectedManagedAnnotations: []string{},
		},
		{
			name:                       "transformed key removal after parent removes pod-only prefix key",
			childLabels:                map[string]string{"prometheus.io/scrape": "true"},
			childAnnotations:           map[string]string{},
			parentLabels:               map[string]string{},
			parentAnnotations:          map[string]string{},
			previousManagedLabels:      []string{"prometheus.io/scrape"},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{},
			expectedManagedAnnotations: []string{},
		},
		{
			name:                       "conflict resolution: prefixed key wins over explicit key (more specific)",
			childLabels:                map[string]string{},
			childAnnotations:           map[string]string{},
			parentLabels:               map[string]string{"prometheus.io/scrape": "false", "pod-only.prometheus.io/scrape": "true"},
			parentAnnotations:          map[string]string{},
			previousManagedLabels:      []string{},
			previousManagedAnnotations: []string{},
			expectedChildLabels:        map[string]string{"prometheus.io/scrape": "true"},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{"prometheus.io/scrape"},
			expectedManagedAnnotations: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      tt.childLabels,
					Annotations: tt.childAnnotations,
				},
			}
			parent := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      tt.parentLabels,
					Annotations: tt.parentAnnotations,
				},
			}

			gotManagedLabels, gotManagedAnnotations := SyncParentMetaToPodTemplate(
				context.Background(),
				child.GetObjectMeta(),
				parent.GetObjectMeta(),
				nil, // protectedLabels - tested separately
				tt.previousManagedLabels,
				tt.previousManagedAnnotations,
			)

			// Verify child labels
			if !reflect.DeepEqual(child.Labels, tt.expectedChildLabels) {
				t.Errorf("SyncParentMetaToPodTemplate() child labels = %v; want %v", child.Labels, tt.expectedChildLabels)
			}

			// Verify child annotations
			if !reflect.DeepEqual(child.Annotations, tt.expectedChildAnnotations) {
				t.Errorf("SyncParentMetaToPodTemplate() child annotations = %v; want %v", child.Annotations, tt.expectedChildAnnotations)
			}

			// Verify managed labels (sorted)
			if !reflect.DeepEqual(gotManagedLabels, tt.expectedManagedLabels) {
				t.Errorf("SyncParentMetaToPodTemplate() managed labels = %v; want %v", gotManagedLabels, tt.expectedManagedLabels)
			}

			// Verify managed annotations (sorted)
			if !reflect.DeepEqual(gotManagedAnnotations, tt.expectedManagedAnnotations) {
				t.Errorf("SyncParentMetaToPodTemplate() managed annotations = %v; want %v", gotManagedAnnotations, tt.expectedManagedAnnotations)
			}
		})
	}
}

func TestSyncParentMetaToPodTemplate_ProtectedLabels(t *testing.T) {
	tests := []struct {
		name                  string
		childLabels           map[string]string
		parentLabels          map[string]string
		protectedLabels       map[string]string
		expectedChildLabels   map[string]string
		expectedManagedLabels []string
	}{
		{
			name:                  "protected labels are not overwritten",
			childLabels:           map[string]string{"app.kubernetes.io/name": "protected-name"},
			parentLabels:          map[string]string{"app.kubernetes.io/name": "overwritten-name", "other": "val"},
			protectedLabels:       map[string]string{"app.kubernetes.io/name": "protected-name"},
			expectedChildLabels:   map[string]string{"app.kubernetes.io/name": "protected-name", "other": "val"},
			expectedManagedLabels: []string{"other"}, // protected label is NOT in managed list
		},
		{
			name:                  "multiple protected labels are preserved",
			childLabels:           map[string]string{"app.kubernetes.io/name": "splunk", "app.kubernetes.io/instance": "test-cr"},
			parentLabels:          map[string]string{"app.kubernetes.io/name": "bad-name", "app.kubernetes.io/instance": "bad-instance", "team": "platform"},
			protectedLabels:       map[string]string{"app.kubernetes.io/name": "splunk", "app.kubernetes.io/instance": "test-cr"},
			expectedChildLabels:   map[string]string{"app.kubernetes.io/name": "splunk", "app.kubernetes.io/instance": "test-cr", "team": "platform"},
			expectedManagedLabels: []string{"team"},
		},
		{
			name:                  "nil protected labels allows all sync",
			childLabels:           map[string]string{"app.kubernetes.io/name": "child-name"},
			parentLabels:          map[string]string{"app.kubernetes.io/name": "parent-name"},
			protectedLabels:       nil,
			expectedChildLabels:   map[string]string{"app.kubernetes.io/name": "parent-name"},
			expectedManagedLabels: []string{"app.kubernetes.io/name"},
		},
		{
			name:                  "empty protected labels allows all sync",
			childLabels:           map[string]string{"app.kubernetes.io/name": "child-name"},
			parentLabels:          map[string]string{"app.kubernetes.io/name": "parent-name"},
			protectedLabels:       map[string]string{},
			expectedChildLabels:   map[string]string{"app.kubernetes.io/name": "parent-name"},
			expectedManagedLabels: []string{"app.kubernetes.io/name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.childLabels,
				},
			}
			parent := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.parentLabels,
				},
			}

			gotManagedLabels, _ := SyncParentMetaToPodTemplate(
				context.Background(),
				child.GetObjectMeta(),
				parent.GetObjectMeta(),
				tt.protectedLabels,
				nil, // previousManagedLabels
				nil, // previousManagedAnnotations
			)

			// Verify child labels
			if !reflect.DeepEqual(child.Labels, tt.expectedChildLabels) {
				t.Errorf("SyncParentMetaToPodTemplate() child labels = %v; want %v", child.Labels, tt.expectedChildLabels)
			}

			// Verify managed labels (sorted)
			if !reflect.DeepEqual(gotManagedLabels, tt.expectedManagedLabels) {
				t.Errorf("SyncParentMetaToPodTemplate() managed labels = %v; want %v", gotManagedLabels, tt.expectedManagedLabels)
			}
		})
	}
}

// TestSyncParentMetaToPodTemplate_ProtectedLabelsNotRemoved verifies that protected labels
// in previousManagedLabels are not removed, even if they're no longer in parent labels.
// This guards against future call sites that might inadvertently track selector labels as managed.
func TestSyncParentMetaToPodTemplate_ProtectedLabelsNotRemoved(t *testing.T) {
	// Scenario: A protected label (e.g., selector label) was somehow tracked in previousManagedLabels.
	// When it's no longer in the CR, the sync should NOT remove it because it's protected.
	child := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app.kubernetes.io/name":     "splunk-indexer", // protected selector label
				"app.kubernetes.io/instance": "test-cr",        // protected selector label
				"team":                       "platform",       // previously managed, now removed from CR
			},
		},
	}
	parent := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				// Note: "team" is intentionally absent (removed from CR)
				// Selector labels not present in parent either
			},
		},
	}
	protectedLabels := map[string]string{
		"app.kubernetes.io/name":     "splunk-indexer",
		"app.kubernetes.io/instance": "test-cr",
	}
	// Simulate a scenario where protected labels were mistakenly added to previousManagedLabels
	previousManagedLabels := []string{"app.kubernetes.io/instance", "app.kubernetes.io/name", "team"}

	gotManagedLabels, _ := SyncParentMetaToPodTemplate(
		context.Background(),
		child.GetObjectMeta(),
		parent.GetObjectMeta(),
		protectedLabels,
		previousManagedLabels,
		nil, // previousManagedAnnotations
	)

	// Expected: protected labels are preserved, "team" is removed (not protected, not in parent)
	expectedChildLabels := map[string]string{
		"app.kubernetes.io/name":     "splunk-indexer",
		"app.kubernetes.io/instance": "test-cr",
		// "team" should be removed
	}
	if !reflect.DeepEqual(child.Labels, expectedChildLabels) {
		t.Errorf("SyncParentMetaToPodTemplate() child labels = %v; want %v", child.Labels, expectedChildLabels)
	}

	// Expected: no managed labels since parent has no eligible labels
	expectedManagedLabels := []string{}
	if !reflect.DeepEqual(gotManagedLabels, expectedManagedLabels) {
		t.Errorf("SyncParentMetaToPodTemplate() managed labels = %v; want %v", gotManagedLabels, expectedManagedLabels)
	}
}

func TestComputeDesiredStatefulSetKeys(t *testing.T) {
	tests := []struct {
		name                string
		parentLabels        map[string]string
		parentAnnotations   map[string]string
		expectedLabels      map[string]string
		expectedAnnotations map[string]string
	}{
		{
			name:                "nil parent metadata",
			parentLabels:        nil,
			parentAnnotations:   nil,
			expectedLabels:      map[string]string{},
			expectedAnnotations: map[string]string{},
		},
		{
			name:                "empty parent metadata",
			parentLabels:        map[string]string{},
			parentAnnotations:   map[string]string{},
			expectedLabels:      map[string]string{},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "regular user labels and annotations pass through",
			parentLabels: map[string]string{
				"team":        "platform",
				"environment": "production",
			},
			parentAnnotations: map[string]string{
				"description": "test service",
				"owner":       "ops-team",
			},
			expectedLabels: map[string]string{
				"team":        "platform",
				"environment": "production",
			},
			expectedAnnotations: map[string]string{
				"description": "test service",
				"owner":       "ops-team",
			},
		},
		{
			name: "kubectl prefix is excluded",
			parentLabels: map[string]string{
				"team": "platform",
				"kubectl.kubernetes.io/last-applied-configuration": "json-data",
			},
			parentAnnotations: map[string]string{
				"description":                       "test",
				"kubectl.kubernetes.io/restartedAt": "2024-01-01",
			},
			expectedLabels: map[string]string{
				"team": "platform",
			},
			expectedAnnotations: map[string]string{
				"description": "test",
			},
		},
		{
			name: "operator prefix is excluded",
			parentLabels: map[string]string{
				"team":                       "platform",
				"operator.splunk.com/status": "active",
			},
			parentAnnotations: map[string]string{
				"description":                  "test",
				"operator.splunk.com/internal": "data",
			},
			expectedLabels: map[string]string{
				"team": "platform",
			},
			expectedAnnotations: map[string]string{
				"description": "test",
			},
		},
		{
			name: "pod-only prefix is excluded for statefulset",
			parentLabels: map[string]string{
				"team":                          "platform",
				"pod-only.prometheus.io/scrape": "true",
			},
			parentAnnotations: map[string]string{
				"description":              "test",
				"pod-only.istio.io/inject": "true",
			},
			expectedLabels: map[string]string{
				"team": "platform",
			},
			expectedAnnotations: map[string]string{
				"description": "test",
			},
		},
		{
			name: "sts-only prefix is transformed by stripping prefix",
			parentLabels: map[string]string{
				"team":                          "platform",
				"sts-only.amadeus.com/priority": "high",
			},
			parentAnnotations: map[string]string{
				"description":                "test",
				"sts-only.example.com/owner": "ops-team",
			},
			expectedLabels: map[string]string{
				"team":                 "platform",
				"amadeus.com/priority": "high",
			},
			expectedAnnotations: map[string]string{
				"description":       "test",
				"example.com/owner": "ops-team",
			},
		},
		{
			name: "mixed scenario with all prefix types",
			parentLabels: map[string]string{
				"team":                               "platform",
				"kubectl.kubernetes.io/last-applied": "config",
				"operator.splunk.com/internal":       "data",
				"pod-only.prometheus.io/scrape":      "true",
				"sts-only.amadeus.com/priority":      "high",
				"mycompany.com/cost-center":          "67890",
			},
			parentAnnotations: map[string]string{
				"description":                       "test",
				"kubectl.kubernetes.io/restartedAt": "2024-01-01",
				"operator.splunk.com/managed":       "true",
				"pod-only.istio.io/inject":          "true",
				"sts-only.example.com/owner":        "ops",
				"mycompany.com/owner":               "team-a",
			},
			expectedLabels: map[string]string{
				"team":                      "platform",
				"amadeus.com/priority":      "high",
				"mycompany.com/cost-center": "67890",
			},
			expectedAnnotations: map[string]string{
				"description":         "test",
				"example.com/owner":   "ops",
				"mycompany.com/owner": "team-a",
			},
		},
		{
			name: "app.kubernetes.io labels pass through",
			parentLabels: map[string]string{
				"app.kubernetes.io/name":    "my-app",
				"app.kubernetes.io/version": "1.0.0",
			},
			parentAnnotations: map[string]string{},
			expectedLabels: map[string]string{
				"app.kubernetes.io/name":    "my-app",
				"app.kubernetes.io/version": "1.0.0",
			},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "conflict resolution: prefixed key wins over explicit key (more specific)",
			parentLabels: map[string]string{
				"amadeus.com/priority":          "low",  // explicit
				"sts-only.amadeus.com/priority": "high", // would transform to same key
			},
			parentAnnotations: map[string]string{
				"example.com/owner":          "team-a", // explicit
				"sts-only.example.com/owner": "team-b", // would transform to same key
			},
			expectedLabels: map[string]string{
				"amadeus.com/priority": "high", // prefixed wins (more specific)
			},
			expectedAnnotations: map[string]string{
				"example.com/owner": "team-b", // prefixed wins (more specific)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      tt.parentLabels,
					Annotations: tt.parentAnnotations,
				},
			}

			gotLabels, gotAnnotations := ComputeDesiredStatefulSetKeys(parent.GetObjectMeta())

			if !reflect.DeepEqual(gotLabels, tt.expectedLabels) {
				t.Errorf("ComputeDesiredStatefulSetKeys() labels = %v; want %v", gotLabels, tt.expectedLabels)
			}
			if !reflect.DeepEqual(gotAnnotations, tt.expectedAnnotations) {
				t.Errorf("ComputeDesiredStatefulSetKeys() annotations = %v; want %v", gotAnnotations, tt.expectedAnnotations)
			}
		})
	}
}

func TestSyncParentMetaToStatefulSet(t *testing.T) {
	selectorLabels := map[string]string{
		"app.kubernetes.io/name":       "indexer",
		"app.kubernetes.io/instance":   "splunk-test-indexer",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/part-of":    "splunk-test-indexer",
	}

	tests := []struct {
		name                       string
		childLabels                map[string]string
		childAnnotations           map[string]string
		parentLabels               map[string]string
		parentAnnotations          map[string]string
		selectorLabels             map[string]string
		expectedChildLabels        map[string]string
		expectedChildAnnotations   map[string]string
		expectedManagedLabels      []string
		expectedManagedAnnotations []string
	}{
		{
			name:              "add new labels and annotations to empty child",
			childLabels:       map[string]string{},
			childAnnotations:  map[string]string{},
			parentLabels:      map[string]string{"team": "platform", "environment": "prod"},
			parentAnnotations: map[string]string{"description": "test"},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"team":        "platform",
				"environment": "prod",
			},
			expectedChildAnnotations: map[string]string{
				"description":                     "test",
				ManagedCRLabelKeysAnnotation:      `["environment","team"]`,
				ManagedCRAnnotationKeysAnnotation: `["description"]`,
			},
			expectedManagedLabels:      []string{"environment", "team"},
			expectedManagedAnnotations: []string{"description"},
		},
		{
			name:              "add new labels to child with nil maps",
			childLabels:       nil,
			childAnnotations:  nil,
			parentLabels:      map[string]string{"team": "platform"},
			parentAnnotations: map[string]string{"owner": "ops"},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"team": "platform",
			},
			expectedChildAnnotations: map[string]string{
				"owner":                           "ops",
				ManagedCRLabelKeysAnnotation:      `["team"]`,
				ManagedCRAnnotationKeysAnnotation: `["owner"]`,
			},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{"owner"},
		},
		{
			name: "update existing managed labels",
			childLabels: map[string]string{
				"team":     "old-team",
				"existing": "keep",
			},
			childAnnotations: map[string]string{
				"description":                ManagedCRLabelKeysAnnotation,
				ManagedCRLabelKeysAnnotation: `["team"]`,
			},
			parentLabels:      map[string]string{"team": "new-team"},
			parentAnnotations: map[string]string{},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"team":     "new-team",
				"existing": "keep",
			},
			expectedChildAnnotations: map[string]string{
				"description":                ManagedCRLabelKeysAnnotation,
				ManagedCRLabelKeysAnnotation: `["team"]`,
			},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{},
		},
		{
			name: "remove previously managed label no longer in parent",
			childLabels: map[string]string{
				"team":     "platform",
				"old-key":  "to-remove",
				"external": "keep",
			},
			childAnnotations: map[string]string{
				"description":                     "test",
				"old-annotation":                  "to-remove",
				ManagedCRLabelKeysAnnotation:      `["old-key","team"]`,
				ManagedCRAnnotationKeysAnnotation: `["description","old-annotation"]`,
			},
			parentLabels:      map[string]string{"team": "platform"},
			parentAnnotations: map[string]string{"description": "test"},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"team":     "platform",
				"external": "keep",
			},
			expectedChildAnnotations: map[string]string{
				"description":                     "test",
				ManagedCRLabelKeysAnnotation:      `["team"]`,
				ManagedCRAnnotationKeysAnnotation: `["description"]`,
			},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{"description"},
		},
		{
			name: "preserve external labels not in previousManaged",
			childLabels: map[string]string{
				"team":           "platform",
				"external-label": "keep-me",
			},
			childAnnotations: map[string]string{
				"external-annotation":        "also-keep",
				ManagedCRLabelKeysAnnotation: `["team"]`,
			},
			parentLabels:      map[string]string{"team": "new-team"},
			parentAnnotations: map[string]string{},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"team":           "new-team",
				"external-label": "keep-me",
			},
			expectedChildAnnotations: map[string]string{
				"external-annotation":        "also-keep",
				ManagedCRLabelKeysAnnotation: `["team"]`,
			},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{},
		},
		{
			name:             "sts-only prefix transformation during sync",
			childLabels:      map[string]string{},
			childAnnotations: map[string]string{},
			parentLabels: map[string]string{
				"sts-only.amadeus.com/priority": "high",
			},
			parentAnnotations: map[string]string{
				"sts-only.example.com/owner": "ops",
			},
			selectorLabels: selectorLabels,
			expectedChildLabels: map[string]string{
				"amadeus.com/priority": "high",
			},
			expectedChildAnnotations: map[string]string{
				"example.com/owner":               "ops",
				ManagedCRLabelKeysAnnotation:      `["amadeus.com/priority"]`,
				ManagedCRAnnotationKeysAnnotation: `["example.com/owner"]`,
			},
			expectedManagedLabels:      []string{"amadeus.com/priority"},
			expectedManagedAnnotations: []string{"example.com/owner"},
		},
		{
			name:             "excluded prefixes are not synced",
			childLabels:      map[string]string{},
			childAnnotations: map[string]string{},
			parentLabels: map[string]string{
				"team":                               "platform",
				"kubectl.kubernetes.io/last-applied": "config",
				"operator.splunk.com/internal":       "data",
				"pod-only.prometheus.io/scrape":      "true",
			},
			parentAnnotations: map[string]string{
				"description":                       "test",
				"kubectl.kubernetes.io/restartedAt": "2024-01-01",
			},
			selectorLabels: selectorLabels,
			expectedChildLabels: map[string]string{
				"team": "platform",
			},
			expectedChildAnnotations: map[string]string{
				"description":                     "test",
				ManagedCRLabelKeysAnnotation:      `["team"]`,
				ManagedCRAnnotationKeysAnnotation: `["description"]`,
			},
			expectedManagedLabels:      []string{"team"},
			expectedManagedAnnotations: []string{"description"},
		},
		{
			name: "selector labels are never removed even if previously managed",
			childLabels: map[string]string{
				"app.kubernetes.io/name":     "indexer",
				"app.kubernetes.io/instance": "splunk-test-indexer",
				"team":                       "old-team",
			},
			childAnnotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["app.kubernetes.io/name","app.kubernetes.io/instance","team"]`,
			},
			parentLabels:      map[string]string{}, // Remove all - but selector labels must stay
			parentAnnotations: map[string]string{},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"app.kubernetes.io/name":     "indexer",
				"app.kubernetes.io/instance": "splunk-test-indexer",
				// "team" is removed because it's not a selector label
			},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{},
			expectedManagedAnnotations: []string{},
		},
		{
			name: "remove all managed keys when parent has none",
			childLabels: map[string]string{
				"team":     "platform",
				"env":      "prod",
				"external": "keep",
			},
			childAnnotations: map[string]string{
				"description":                     "test",
				"owner":                           "ops",
				ManagedCRLabelKeysAnnotation:      `["env","team"]`,
				ManagedCRAnnotationKeysAnnotation: `["description","owner"]`,
			},
			parentLabels:      map[string]string{},
			parentAnnotations: map[string]string{},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"external": "keep",
			},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{},
			expectedManagedAnnotations: []string{},
		},
		{
			name: "add update and remove in single sync",
			childLabels: map[string]string{
				"keep":   "v1",
				"update": "old",
				"remove": "gone",
			},
			childAnnotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["keep","remove","update"]`,
			},
			parentLabels: map[string]string{
				"keep":   "v1",
				"update": "new",
				"add":    "fresh",
			},
			parentAnnotations: map[string]string{},
			selectorLabels:    selectorLabels,
			expectedChildLabels: map[string]string{
				"keep":   "v1",
				"update": "new",
				"add":    "fresh",
			},
			expectedChildAnnotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["add","keep","update"]`,
			},
			expectedManagedLabels:      []string{"add", "keep", "update"},
			expectedManagedAnnotations: []string{},
		},
		{
			name: "transformed key removal after parent removes sts-only prefix key",
			childLabels: map[string]string{
				"amadeus.com/priority": "high",
			},
			childAnnotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["amadeus.com/priority"]`,
			},
			parentLabels:               map[string]string{},
			parentAnnotations:          map[string]string{},
			selectorLabels:             selectorLabels,
			expectedChildLabels:        map[string]string{},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{},
			expectedManagedAnnotations: []string{},
		},
		{
			name: "nil selector labels - still works",
			childLabels: map[string]string{
				"team": "platform",
			},
			childAnnotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["team"]`,
			},
			parentLabels:               map[string]string{},
			parentAnnotations:          map[string]string{},
			selectorLabels:             nil,
			expectedChildLabels:        map[string]string{},
			expectedChildAnnotations:   map[string]string{},
			expectedManagedLabels:      []string{},
			expectedManagedAnnotations: []string{},
		},
		{
			name:             "conflict resolution: prefixed key wins over explicit key (more specific)",
			childLabels:      map[string]string{},
			childAnnotations: map[string]string{},
			parentLabels: map[string]string{
				"amadeus.com/priority":          "low",
				"sts-only.amadeus.com/priority": "high",
			},
			parentAnnotations:   map[string]string{},
			selectorLabels:      selectorLabels,
			expectedChildLabels: map[string]string{"amadeus.com/priority": "high"},
			expectedChildAnnotations: map[string]string{
				ManagedCRLabelKeysAnnotation: `["amadeus.com/priority"]`,
			},
			expectedManagedLabels:      []string{"amadeus.com/priority"},
			expectedManagedAnnotations: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      tt.childLabels,
					Annotations: tt.childAnnotations,
				},
			}
			parent := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      tt.parentLabels,
					Annotations: tt.parentAnnotations,
				},
			}

			SyncParentMetaToStatefulSet(context.Background(), child.GetObjectMeta(), parent.GetObjectMeta(), tt.selectorLabels)

			// Verify child labels
			if !reflect.DeepEqual(child.Labels, tt.expectedChildLabels) {
				t.Errorf("SyncParentMetaToStatefulSet() child labels = %v; want %v", child.Labels, tt.expectedChildLabels)
			}

			// Verify managed labels (sorted)
			gotManagedLabels := GetManagedLabelKeys(child.Annotations)
			gotManagedAnnotations := GetManagedAnnotationKeys(child.Annotations)

			// Verify managed labels (sorted)
			if !reflect.DeepEqual(gotManagedLabels, tt.expectedManagedLabels) {
				t.Errorf("SyncParentMetaToStatefulSet() managed labels = %v; want %v", gotManagedLabels, tt.expectedManagedLabels)
			}

			// Verify managed annotations (sorted)
			if !reflect.DeepEqual(gotManagedAnnotations, tt.expectedManagedAnnotations) {
				t.Errorf("SyncParentMetaToStatefulSet() managed annotations = %v; want %v", gotManagedAnnotations, tt.expectedManagedAnnotations)
			}
		})
	}
}

func TestParseResourceQuantity(t *testing.T) {
	resourceQuantityTester := func(t *testing.T, str string, defaultStr string, want int64) {
		q, err := ParseResourceQuantity(str, defaultStr)
		if err != nil {
			t.Errorf("ParseResourceQuantity(\"%s\",\"%s\") error: %v", str, defaultStr, err)
		}

		got, success := q.AsInt64()
		if !success {
			t.Errorf("ParseResourceQuantity(\"%s\",\"%s\") returned false", str, defaultStr)
		}
		if got != want {
			t.Errorf("ParseResourceQuantity(\"%s\",\"%s\") = %d; want %d", str, defaultStr, got, want)
		}
	}

	resourceQuantityTester(t, "1Gi", "", 1073741824)
	resourceQuantityTester(t, "4", "", 4)
	resourceQuantityTester(t, "", "1Gi", 1073741824)

	_, err := ParseResourceQuantity("13rf1", "")
	if err == nil {
		t.Errorf("ParseResourceQuantity(\"13rf1\",\"\") returned nil; want error")
	}
}

func TestGetServiceFQDN(t *testing.T) {
	test := func(namespace string, name string, want string) {
		got := GetServiceFQDN(namespace, name)
		if got != want {
			t.Errorf("GetServiceFQDN() = %s; want %s", got, want)
		}
	}

	test("test", "t1", "t1.test.svc.cluster.local")

	os.Setenv("CLUSTER_DOMAIN", "example.com")
	test("test", "t2", "t2.test.svc.example.com")
}

func TestGenerateSecret(t *testing.T) {
	test := func(SecretBytes string, n int) {
		results := [][]byte{}

		// get 10 results
		for i := 0; i < 10; i++ {
			results = append(results, GenerateSecret(SecretBytes, n))

			// ensure its length is correct
			if len(results[i]) != n {
				t.Errorf("GenerateSecret(\"%s\",%d) len = %d; want %d", SecretBytes, 10, len(results[i]), n)
			}

			// ensure it only includes allowed bytes
			for _, c := range results[i] {
				if bytes.IndexByte([]byte(SecretBytes), c) == -1 {
					t.Errorf("GenerateSecret(\"%s\",%d) returned invalid byte: %c", SecretBytes, 10, c)
				}
			}

			// ensure each result is unique
			for x := i; x > 0; x-- {
				if bytes.Equal(results[x-1], results[i]) {
					t.Errorf("GenerateSecret(\"%s\",%d) returned two identical values: %s", SecretBytes, n, string(results[i]))
				}
			}
		}
	}

	test("ABCDEF01234567890", 10)
	test("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 10)
}

func TestSortContainerPorts(t *testing.T) {
	var ports []corev1.ContainerPort
	var want []corev1.ContainerPort

	test := func() {
		got := SortContainerPorts(ports)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SortContainerPorts() got %v; want %v", got, want)
		}
	}

	ports = []corev1.ContainerPort{
		{ContainerPort: 100},
		{ContainerPort: 200},
		{ContainerPort: 3000},
	}
	want = ports
	test()

	ports = []corev1.ContainerPort{
		{ContainerPort: 3000},
		{ContainerPort: 100},
		{ContainerPort: 200},
	}
	want = []corev1.ContainerPort{
		{ContainerPort: 100},
		{ContainerPort: 200},
		{ContainerPort: 3000},
	}
	test()
}

func TestSortSlice(t *testing.T) {
	type uintst struct {
		value uint
	}
	sluint := []uintst{{1}, {2}, {3}}
	SortSlice(sluint, "value")

	type fltst struct {
		value float32
	}
	slflt := []fltst{{1.0}, {2.0}, {3.0}}
	SortSlice(slflt, "value")
}

func TestSortServicePorts(t *testing.T) {
	var ports []corev1.ServicePort
	var want []corev1.ServicePort

	test := func() {
		got := SortServicePorts(ports)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SortServicePorts() got %v; want %v", got, want)
		}
	}

	ports = []corev1.ServicePort{
		{Port: 100},
		{Port: 200},
		{Port: 3000},
	}
	want = ports
	test()

	ports = []corev1.ServicePort{
		{Port: 3000},
		{Port: 100},
		{Port: 200},
	}
	want = []corev1.ServicePort{
		{Port: 100},
		{Port: 200},
		{Port: 3000},
	}
	test()
}

func compareTester(t *testing.T, method string, f func() bool, a interface{}, b interface{}, want bool) {
	got := f()
	if got != want {
		aBytes, errA := json.Marshal(a)
		bBytes, errB := json.Marshal(b)
		cmp := "=="
		if want {
			cmp = "!="
		}
		if errA == nil && errB == nil {
			t.Errorf("%s() failed: %s %s %s", method, string(aBytes), cmp, string(bBytes))
		} else {
			t.Errorf("%s() failed: %v %s %v", method, a, cmp, b)
		}
	}
}

func TestCompareImagePullSecrets(t *testing.T) {
	a := []corev1.LocalObjectReference{
		{Name: "abc"},
	}

	b := []corev1.LocalObjectReference{
		{Name: "abc"},
	}

	if CompareImagePullSecrets(a, b) {
		t.Errorf("Unequal imagepullsecrets, should return a difference")
	}

}

func TestCompareContainerPorts(t *testing.T) {
	var a []corev1.ContainerPort
	var b []corev1.ContainerPort

	test := func(want bool) {
		f := func() bool {
			return CompareContainerPorts(a, b)
		}
		compareTester(t, "CompareContainerPorts", f, a, b, want)
	}

	test(false)

	var nullPort corev1.ContainerPort
	httpPort := corev1.ContainerPort{
		Name:          "http",
		ContainerPort: 80,
		Protocol:      "TCP",
	}
	splunkWebPort := corev1.ContainerPort{
		Name:          "splunk",
		ContainerPort: 8000,
		Protocol:      "TCP",
	}
	s2sPort := corev1.ContainerPort{
		Name:          "s2s",
		ContainerPort: 9997,
		Protocol:      "TCP",
	}

	a = []corev1.ContainerPort{nullPort, nullPort}
	b = []corev1.ContainerPort{nullPort, nullPort}
	test(false)

	a = []corev1.ContainerPort{nullPort, nullPort}
	b = []corev1.ContainerPort{nullPort, nullPort, nullPort}
	test(true)

	a = []corev1.ContainerPort{httpPort, splunkWebPort}
	b = []corev1.ContainerPort{httpPort, splunkWebPort}
	test(false)

	a = []corev1.ContainerPort{httpPort, s2sPort, splunkWebPort}
	b = []corev1.ContainerPort{s2sPort, splunkWebPort, httpPort}
	test(false)

	a = []corev1.ContainerPort{httpPort, s2sPort}
	b = []corev1.ContainerPort{s2sPort, splunkWebPort}
	test(true)

	a = []corev1.ContainerPort{s2sPort}
	b = []corev1.ContainerPort{s2sPort, splunkWebPort}
	test(true)
}

func TestCompareServicePorts(t *testing.T) {
	var a []corev1.ServicePort
	var b []corev1.ServicePort

	test := func(want bool) {
		f := func() bool {
			return CompareServicePorts(a, b)
		}
		compareTester(t, "CompareServicePorts", f, a, b, want)
	}

	test(false)

	var nullPort corev1.ServicePort
	httpPort := corev1.ServicePort{
		Name:     "http",
		Port:     80,
		Protocol: "TCP",
	}
	splunkWebPort := corev1.ServicePort{
		Name:     "splunk",
		Port:     8000,
		Protocol: "TCP",
	}
	s2sPort := corev1.ServicePort{
		Name:     "s2s",
		Port:     9997,
		Protocol: "TCP",
	}

	a = []corev1.ServicePort{nullPort, nullPort}
	b = []corev1.ServicePort{nullPort, nullPort}
	test(false)

	a = []corev1.ServicePort{nullPort, nullPort}
	b = []corev1.ServicePort{nullPort, nullPort, nullPort}
	test(true)

	a = []corev1.ServicePort{httpPort, splunkWebPort}
	b = []corev1.ServicePort{httpPort, splunkWebPort}
	test(false)

	a = []corev1.ServicePort{httpPort, s2sPort, splunkWebPort}
	b = []corev1.ServicePort{s2sPort, splunkWebPort, httpPort}
	test(false)

	a = []corev1.ServicePort{httpPort, s2sPort}
	b = []corev1.ServicePort{s2sPort, splunkWebPort}
	test(true)

	a = []corev1.ServicePort{s2sPort}
	b = []corev1.ServicePort{s2sPort, splunkWebPort}
	test(true)
}

func TestCompareEnvs(t *testing.T) {
	var a []corev1.EnvVar
	var b []corev1.EnvVar

	test := func(want bool) {
		f := func() bool {
			return CompareEnvs(a, b)
		}
		compareTester(t, "CompareEnvs", f, a, b, want)
	}

	test(false)

	aEnv := corev1.EnvVar{
		Name:  "A",
		Value: "a",
	}
	bEnv := corev1.EnvVar{
		Name:  "B",
		Value: "b",
	}
	cEnv := corev1.EnvVar{
		Name:  "C",
		Value: "c",
	}

	a = []corev1.EnvVar{aEnv, bEnv}
	b = []corev1.EnvVar{aEnv, bEnv}
	test(false)

	a = []corev1.EnvVar{aEnv, cEnv, bEnv}
	b = []corev1.EnvVar{cEnv, bEnv, aEnv}
	test(false)

	a = []corev1.EnvVar{aEnv, cEnv}
	b = []corev1.EnvVar{cEnv, bEnv}
	test(true)

	a = []corev1.EnvVar{aEnv, cEnv}
	b = []corev1.EnvVar{cEnv}
	test(true)
}

func TestCompareVolumeMounts(t *testing.T) {
	var a []corev1.VolumeMount
	var b []corev1.VolumeMount

	test := func(want bool) {
		f := func() bool {
			return CompareVolumeMounts(a, b)
		}
		compareTester(t, "CompareVolumeMounts", f, a, b, want)
	}

	test(false)

	var nullVolume corev1.VolumeMount
	varVolume := corev1.VolumeMount{
		Name:      "mnt-var",
		MountPath: "/opt/splunk/var",
	}
	etcVolume := corev1.VolumeMount{
		Name:      "mnt-etc",
		MountPath: "/opt/splunk/etc",
	}
	secretVolume := corev1.VolumeMount{
		Name:      "mnt-secrets",
		MountPath: "/mnt/secrets",
	}

	a = []corev1.VolumeMount{nullVolume, nullVolume}
	b = []corev1.VolumeMount{nullVolume, nullVolume}
	test(false)

	a = []corev1.VolumeMount{nullVolume, nullVolume}
	b = []corev1.VolumeMount{nullVolume, nullVolume, nullVolume}
	test(true)

	a = []corev1.VolumeMount{varVolume, etcVolume}
	b = []corev1.VolumeMount{varVolume, etcVolume}
	test(false)

	a = []corev1.VolumeMount{varVolume, secretVolume, etcVolume}
	b = []corev1.VolumeMount{secretVolume, etcVolume, varVolume}
	test(false)

	a = []corev1.VolumeMount{varVolume, secretVolume}
	b = []corev1.VolumeMount{secretVolume, etcVolume}
	test(true)
}

func TestCompareByMarshall(t *testing.T) {
	var a corev1.ResourceRequirements
	var b corev1.ResourceRequirements

	test := func(want bool) {
		f := func() bool {
			return CompareByMarshall(a, b)
		}
		compareTester(t, "CompareByMarshall", f, a, b, want)
	}

	test(false)

	low := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("0.1"),
		corev1.ResourceMemory: resource.MustParse("512Mi"),
	}
	medium := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("8"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	}
	high := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("32"),
		corev1.ResourceMemory: resource.MustParse("32Gi"),
	}

	a = corev1.ResourceRequirements{Requests: low, Limits: high}
	b = corev1.ResourceRequirements{Requests: low, Limits: high}
	test(false)

	a = corev1.ResourceRequirements{Requests: medium, Limits: high}
	b = corev1.ResourceRequirements{Requests: low, Limits: high}
	test(true)

	// Negative testing
	if !CompareByMarshall(math.Inf(1), 1) {
		t.Errorf("Invalid marshall run also returns true")
	}
	if !CompareByMarshall(1, math.Inf(1)) {
		t.Errorf("Invalid marshall run also returns true")
	}
}

func TestCompareSortedStrings(t *testing.T) {
	var a []string
	var b []string

	test := func(want bool) {
		f := func() bool {
			return CompareSortedStrings(a, b)
		}
		compareTester(t, "CompareSortedStrings", f, a, b, want)
	}

	test(false)

	ip1 := "192.168.2.1"
	ip2 := "192.168.2.100"
	ip3 := "192.168.10.1"

	a = []string{ip1, ip2}
	b = []string{ip1, ip2}
	test(false)

	a = []string{ip1, ip3, ip2}
	b = []string{ip3, ip2, ip1}
	test(false)

	a = []string{ip1, ip3}
	b = []string{ip3, ip2}
	test(true)

	a = []string{ip1, ip3}
	b = []string{ip3}
	test(true)
}

func TestGetIstioAnnotations(t *testing.T) {
	var ports []corev1.ContainerPort
	var want map[string]string

	test := func() {
		got := GetIstioAnnotations(ports)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetIstioAnnotations() = %v; want %v", got, want)
		}
	}

	ports = []corev1.ContainerPort{
		{ContainerPort: 8000}, {ContainerPort: 80},
	}
	want = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "80,8000",
	}
	test()

	ports = []corev1.ContainerPort{
		{ContainerPort: 8089}, {ContainerPort: 8191},
	}
	want = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "",
	}
	test()
}

func TestGetLabels(t *testing.T) {
	test := func(component, name, instanceIdentifier string, partOfIdentifier string, want map[string]string) {
		got, _ := GetLabels(component, name, instanceIdentifier, partOfIdentifier, make([]string, 0))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetLabels(\"%s\",\"%s\",\"%s\",\"%s\") = %v; want %v", component, name, instanceIdentifier, partOfIdentifier, got, want)
		}
	}

	test("indexer", "cluster-manager", "t1", "t1", map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "cluster-manager",
		"app.kubernetes.io/part-of":    "splunk-t1-indexer",
		"app.kubernetes.io/instance":   "splunk-t1-cluster-manager",
	})

	// Multipart IndexerCluster - selector of indexer service for main part
	test("indexer", "indexer", "", "cluster1", map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "indexer",
		"app.kubernetes.io/part-of":    "splunk-cluster1-indexer",
	})

	// Multipart IndexerCluster - labels of child IndexerCluster part
	test("indexer", "indexer", "site1", "cluster1", map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "indexer",
		"app.kubernetes.io/part-of":    "splunk-cluster1-indexer",
		"app.kubernetes.io/instance":   "splunk-site1-indexer",
	})

	testNew := func(component, name, instanceIdentifier string, partOfIdentifier string, selectFew []string, want map[string]string, expectedErr string) {
		got, err := GetLabels(component, name, instanceIdentifier, partOfIdentifier, selectFew)
		if err != nil && expectedErr != err.Error() {
			t.Errorf("GetLabels(\"%s\",\"%s\",\"%s\",\"%s\") expected Error %s, got error %s", component, name, instanceIdentifier, partOfIdentifier, expectedErr, err.Error())
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetLabels(\"%s\",\"%s\",\"%s\",\"%s\") = %v; want %v", component, name, instanceIdentifier, partOfIdentifier, got, want)
		}
	}

	// Test all labels using selectFew option
	selectAll := []string{"manager", "component", "name", "partof", "instance"}
	testNew("indexer", "cluster-manager", "t1", "t1", selectAll, map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "cluster-manager",
		"app.kubernetes.io/part-of":    "splunk-t1-indexer",
		"app.kubernetes.io/instance":   "splunk-t1-cluster-manager",
	}, "")

	// Test a few labels using selectFew option
	selectFewPartial := []string{"manager", "component"}
	testNew("indexer", "cluster-manager", "t1", "t1", selectFewPartial, map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
	}, "")

	// Test incorrect label
	selectFewIncorrect := []string{"randomvalue"}
	testNew("indexer", "cluster-manager", "t1", "t1", selectFewIncorrect, map[string]string{}, "Incorrect label type randomvalue")
}

func TestAppendPodAffinity(t *testing.T) {
	var affinity corev1.Affinity
	identifier := "test1"
	typeLabel := "indexer"

	test := func(want corev1.Affinity) {
		got := AppendPodAntiAffinity(&affinity, identifier, typeLabel)
		f := func() bool {
			return CompareByMarshall(got, want)
		}
		compareTester(t, "AppendPodAntiAffinity()", f, got, want, false)
	}

	wantAppended := corev1.WeightedPodAffinityTerm{
		Weight: 100,
		PodAffinityTerm: corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app.kubernetes.io/instance",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{fmt.Sprintf("splunk-%s-%s", identifier, typeLabel)},
					},
				},
			},
			TopologyKey: "kubernetes.io/hostname",
		},
	}

	test(corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				wantAppended,
			},
		},
	})

	affinity = corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{Namespaces: []string{"test"}},
			},
		},
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				wantAppended,
			},
		},
	}
	test(corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{Namespaces: []string{"test"}},
			},
		},
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				wantAppended, wantAppended,
			},
		},
	})

	// Negative test
	aff := AppendPodAntiAffinity(nil, "", "")
	if aff == nil {
		t.Errorf("Passing an empty affinity also should return some form of affinity")
	}
}

func TestCompareTolerations(t *testing.T) {
	var a []corev1.Toleration
	var b []corev1.Toleration

	test := func(want bool) {
		f := func() bool {
			return CompareTolerations(a, b)
		}
		compareTester(t, "CompareTolerations", f, a, b, want)
	}

	// No change
	test(false)
	var nullToleration corev1.Toleration

	toleration1 := corev1.Toleration{
		Key:      "key1",
		Operator: corev1.TolerationOpEqual,
		Value:    "value1",
		Effect:   corev1.TaintEffectNoSchedule,
	}
	toleration2 := corev1.Toleration{
		Key:      "key1",
		Operator: corev1.TolerationOpEqual,
		Value:    "value1",
		Effect:   corev1.TaintEffectNoSchedule,
	}

	// No change
	a = []corev1.Toleration{nullToleration, nullToleration}
	b = []corev1.Toleration{nullToleration, nullToleration}
	test(false)

	// No change
	a = []corev1.Toleration{toleration1}
	b = []corev1.Toleration{toleration2}
	test(false)

	// Change effect
	var toleration corev1.Toleration

	toleration = toleration2
	toleration.Effect = corev1.TaintEffectNoExecute
	a = []corev1.Toleration{toleration}
	b = []corev1.Toleration{toleration1}
	test(true)

	// Change operator
	toleration = toleration2
	toleration.Operator = corev1.TolerationOpExists
	a = []corev1.Toleration{toleration}
	b = []corev1.Toleration{toleration1}
	test(true)

	// Change value
	toleration = toleration2
	toleration.Value = "newValue"
	a = []corev1.Toleration{toleration}
	b = []corev1.Toleration{toleration1}
	test(true)

}

func TestCompareTopologySpreadConstraints(t *testing.T) {
	var a []corev1.TopologySpreadConstraint
	var b []corev1.TopologySpreadConstraint

	test := func(want bool) {
		f := func() bool {
			return CompareTopologySpreadConstraints(a, b)
		}
		compareTester(t, "CompareTopologySpreadConstraints", f, a, b, want)
	}

	// No change
	test(false)

	var nullTopologySpreadConstraint corev1.TopologySpreadConstraint
	topologySpreadConstraint1 := corev1.TopologySpreadConstraint{
		MaxSkew:           1,
		TopologyKey:       "key1",
		WhenUnsatisfiable: "key2",
		//LabelSelector: <object>
		//MatchLabelKeys: <list> # optional; alpha since v1.25
		//NodeAffinityPolicy: [Honor|Ignore] # optional; beta since v1.26
		//NodeTaintsPolicy:
	}
	topologySpreadConstraint2 := corev1.TopologySpreadConstraint{
		MaxSkew:           1,
		TopologyKey:       "key1",
		WhenUnsatisfiable: "key2",
		//LabelSelector: <object>
		//MatchLabelKeys: <list> # optional; alpha since v1.25
		//NodeAffinityPolicy: [Honor|Ignore] # optional; beta since v1.26
		//NodeTaintsPolicy:
	}

	// No change
	a = []corev1.TopologySpreadConstraint{nullTopologySpreadConstraint, nullTopologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{nullTopologySpreadConstraint, nullTopologySpreadConstraint}
	test(false)

	// No change
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint2}
	test(false)

	// Change maxSkew
	var topologySpreadConstraint corev1.TopologySpreadConstraint

	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.MaxSkew = 1
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(false)

	// Change topologyKey
	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.TopologyKey = ""
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(true)

	// Change labelSelector
	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.LabelSelector = &metav1.LabelSelector{}
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(true)

	// Change matchLabelKeys
	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.MatchLabelKeys = []string{"newValue"}
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(true)

	/*
		// Change nodeAffinityPolicy
		topologySpreadConstraint = topologySpreadConstraint2
		topologySpreadConstraint.NodeAffinityPolicy = &corev1.NodeInclusionPolicy{}
		a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
		b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
		test(true)

		// Change nodeTaintsPolicy
		topologySpreadConstraint = topologySpreadConstraint2
		topologySpreadConstraint.NodeTaintsPolicy = &corev1.NodeInclusionPolicy{}
		a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
		b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
		test(true)
	*/
}

func TestCompareVolumes(t *testing.T) {
	var a []corev1.Volume
	var b []corev1.Volume

	test := func(want bool) {
		f := func() bool {
			return CompareVolumes(a, b)
		}
		compareTester(t, "CompareVolumes", f, a, b, want)
	}

	// No change
	test(false)

	var nullVolume corev1.Volume

	defaultMode := int32(440)
	secret1Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret1"}}}
	secret2Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret2"}}}
	secret3Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret2", DefaultMode: &defaultMode}}}
	secret4Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret2", DefaultMode: &defaultMode}}}

	// No change
	a = []corev1.Volume{nullVolume, nullVolume}
	b = []corev1.Volume{nullVolume, nullVolume}
	test(false)

	// Change - new volume
	a = []corev1.Volume{nullVolume, nullVolume}
	b = []corev1.Volume{nullVolume, nullVolume, nullVolume}
	test(true)

	// No change
	a = []corev1.Volume{secret1Volume}
	b = []corev1.Volume{secret1Volume}
	test(false)

	// Change - new volume
	a = []corev1.Volume{secret1Volume}
	b = []corev1.Volume{secret1Volume, secret2Volume}
	test(true)

	// Change - new default mode
	a = []corev1.Volume{secret2Volume}
	b = []corev1.Volume{secret3Volume}
	test(true)

	// No change
	a = []corev1.Volume{secret3Volume}
	b = []corev1.Volume{secret4Volume}
	test(false)
}

func TestSortAndCompareSlices(t *testing.T) {

	// Test panic cases
	var done sync.WaitGroup

	var deferFunc = func() {
		if r := recover(); r == nil {
			t.Errorf("Expect code panic when comparing slices")
		}
		done.Done()
	}

	done.Add(1)
	go func() {
		var a corev1.ServicePort
		var b corev1.ContainerPort
		defer deferFunc()
		sortAndCompareSlices(a, b, "Name")
	}()

	done.Add(1)
	go func() {
		var a []corev1.ServicePort
		var b []corev1.ContainerPort

		defer deferFunc()
		sortAndCompareSlices(a, b, "Name")
	}()
	done.Add(1)
	go func() {
		defer deferFunc()
		a := []corev1.ServicePort{{Name: "http", Port: 80}}
		b := []corev1.ServicePort{{Name: "http", Port: 80}}
		sortAndCompareSlices(a, b, "SortFieldNameDoesNotExist")
	}()
	done.Wait()

	// Test inequality
	var a []corev1.ServicePort
	var b []corev1.ServicePort
	a = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	b = []corev1.ServicePort{{Name: "http", Port: 81}, {Name: "https", Port: 443}}
	if !sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be not equal - (%v, %v)", a, b)
	}

	a = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	b = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}, {Name: "ssh", Port: 22}}
	if !sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be not equal - (%v, %v)", a, b)
	}

	// Test equality
	a = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	b = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	if sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be equal - (%v, %v)", a, b)
	}

	a = []corev1.ServicePort{{Name: "ssh", Port: 22}, {Name: "https", Port: 443}, {Name: "http", Port: 80}}
	b = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}, {Name: "ssh", Port: 22}}
	if sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be equal - (%v, %v)", a, b)
	}
}
