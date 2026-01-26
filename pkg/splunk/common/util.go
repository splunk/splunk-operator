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
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func init() {
	// seed random number generator for splunk secret generation
	rand.Seed(time.Now().UnixNano())
}

// AsOwner returns an object to use for Kubernetes resource ownership references.
func AsOwner(cr MetaObject, isController bool) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: cr.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		Kind:       cr.GetObjectKind().GroupVersionKind().Kind,
		Name:       cr.GetObjectMeta().GetName(),
		UID:        cr.GetObjectMeta().GetUID(),
		Controller: &isController,
	}
}

// Prefix constants for selective metadata propagation.
// These prefixes allow users to specify metadata that should only appear on specific resource types.
//
// Usage:
//   - pod-only.prometheus.io/scrape: "true"  → appears as prometheus.io/scrape: "true" on pods only
//   - sts-only.amadeus.com/priority: "high"  → appears as amadeus.com/priority: "high" on StatefulSet only
const (
	// podOnlyPrefix is stripped during Pod Template propagation.
	// Example: "pod-only.prometheus.io/scrape" → "prometheus.io/scrape"
	podOnlyPrefix = "pod-only."

	// stsOnlyPrefix is stripped during StatefulSet propagation.
	// Example: "sts-only.amadeus.com/priority" → "amadeus.com/priority"
	stsOnlyPrefix = "sts-only."
)

// podTemplateExcludedPrefixes defines prefixes excluded from Pod Template metadata propagation.
// Labels/annotations with these prefixes will NOT be copied from the CR to Pod Template.
//
// The exclusion rules are:
//   - kubectl.kubernetes.io/*: kubectl-managed metadata (e.g., last-applied-configuration)
//   - operator.splunk.com/*: Operator-internal metadata not meant for pods
//   - sts-only.*: StatefulSet-only labels that should not propagate to pods
//
// Note: pod-only.* keys are NOT excluded - they are transformed (prefix stripped)
// during propagation, allowing users to specify pod-specific metadata on the CR.
var podTemplateExcludedPrefixes = []string{
	"kubectl.kubernetes.io/",
	"operator.splunk.com/",
	stsOnlyPrefix, // StatefulSet-only labels don't go to pods
}

// statefulSetExcludedPrefixes defines prefixes excluded from StatefulSet ObjectMeta propagation.
// Labels/annotations with these prefixes will NOT be copied from the CR to StatefulSet metadata.
//
// The exclusion rules are:
//   - kubectl.kubernetes.io/*: kubectl-managed metadata (e.g., last-applied-configuration)
//   - operator.splunk.com/*: Operator-internal metadata not meant for StatefulSet
//   - pod-only.*: Pod-only labels that should not propagate to StatefulSet
//
// Note: sts-only.* keys are NOT excluded - they are transformed (prefix stripped)
// during propagation, allowing users to specify StatefulSet-specific metadata on the CR.
var statefulSetExcludedPrefixes = []string{
	"kubectl.kubernetes.io/",
	"operator.splunk.com/",
	podOnlyPrefix, // Pod-only labels don't go to StatefulSet
}

// Tracking Annotations for Metadata Sync
//
// These annotation keys store JSON arrays of keys that were propagated from the CR (Custom Resource)
// to child resources (StatefulSet, Pod Template). They enable "sync" semantics rather than
// "append-only" semantics:
//
// SYNC BEHAVIOR (new):
//   - Keys added to CR are propagated to child resources
//   - Keys updated on CR are updated on child resources
//   - Keys REMOVED from CR are REMOVED from child resources (if previously managed)
//
// APPEND-ONLY BEHAVIOR (old - used by AppendParentMeta):
//   - Keys added to CR are propagated to child resources
//   - Keys updated on CR may NOT update child resources (no-clobber)
//   - Keys removed from CR are NOT removed from child resources
//
// By tracking which keys were propagated, the operator can distinguish between:
//   - CR-managed keys: Can be safely removed when removed from CR
//   - External keys: Applied by users/tools, must be preserved
//
// The annotations store sorted JSON arrays for deterministic comparison, e.g.:
//
//	["team","environment","cost-center"]
const (
	// ManagedCRLabelKeysAnnotation tracks which label keys were propagated from CR metadata.
	// Value is a JSON array of label key strings, e.g., ["team","environment"].
	// Used by SyncParentMetaToStatefulSet to identify keys that can be removed.
	ManagedCRLabelKeysAnnotation = "operator.splunk.com/managed-cr-label-keys"

	// ManagedCRAnnotationKeysAnnotation tracks which annotation keys were propagated from CR metadata.
	// Value is a JSON array of annotation key strings.
	// Used by SyncParentMetaToStatefulSet to identify keys that can be removed.
	ManagedCRAnnotationKeysAnnotation = "operator.splunk.com/managed-cr-annotation-keys"
)

// GetManagedLabelKeys returns the list of label keys that were previously propagated from CR.
// It parses the JSON array stored in the ManagedCRLabelKeysAnnotation.
// Returns an empty slice if the annotation is missing, empty, or contains invalid JSON.
func GetManagedLabelKeys(annotations map[string]string) []string {
	if annotations == nil {
		return []string{}
	}
	value, exists := annotations[ManagedCRLabelKeysAnnotation]
	if !exists || value == "" {
		return []string{}
	}
	var keys []string
	if err := json.Unmarshal([]byte(value), &keys); err != nil {
		return []string{}
	}
	return keys
}

// GetManagedAnnotationKeys returns the list of annotation keys that were previously propagated from CR.
// It parses the JSON array stored in the ManagedCRAnnotationKeysAnnotation.
// Returns an empty slice if the annotation is missing, empty, or contains invalid JSON.
func GetManagedAnnotationKeys(annotations map[string]string) []string {
	if annotations == nil {
		return []string{}
	}
	value, exists := annotations[ManagedCRAnnotationKeysAnnotation]
	if !exists || value == "" {
		return []string{}
	}
	var keys []string
	if err := json.Unmarshal([]byte(value), &keys); err != nil {
		return []string{}
	}
	return keys
}

// SetManagedLabelKeys stores the list of label keys that were propagated from CR.
// It serializes the keys as a sorted JSON array and stores it in ManagedCRLabelKeysAnnotation.
// If keys is nil or empty, the annotation is removed.
// The annotations map must not be nil.
func SetManagedLabelKeys(annotations map[string]string, keys []string) {
	if annotations == nil {
		return
	}
	if len(keys) == 0 {
		delete(annotations, ManagedCRLabelKeysAnnotation)
		return
	}
	// Sort keys for deterministic output
	sortedKeys := make([]string, len(keys))
	copy(sortedKeys, keys)
	sort.Strings(sortedKeys)
	// Serialize to JSON
	data, err := json.Marshal(sortedKeys)
	if err != nil {
		return
	}
	annotations[ManagedCRLabelKeysAnnotation] = string(data)
}

// SetManagedAnnotationKeys stores the list of annotation keys that were propagated from CR.
// It serializes the keys as a sorted JSON array and stores it in ManagedCRAnnotationKeysAnnotation.
// If keys is nil or empty, the annotation is removed.
// The annotations map must not be nil.
func SetManagedAnnotationKeys(annotations map[string]string, keys []string) {
	if annotations == nil {
		return
	}
	if len(keys) == 0 {
		delete(annotations, ManagedCRAnnotationKeysAnnotation)
		return
	}
	// Sort keys for deterministic output
	sortedKeys := make([]string, len(keys))
	copy(sortedKeys, keys)
	sort.Strings(sortedKeys)
	// Serialize to JSON
	data, err := json.Marshal(sortedKeys)
	if err != nil {
		return
	}
	annotations[ManagedCRAnnotationKeysAnnotation] = string(data)
}

// hasExcludedPrefix checks if key starts with any excluded prefix
func hasExcludedPrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// IsManagedKey returns true if a key is "managed" (can be propagated from CR).
// A key is managed if it does NOT have any of the excluded prefixes.
//
// Managed keys are user-defined labels/annotations that should be synced to child resources.
// Examples of managed keys:
//   - "team" (plain user label)
//   - "environment" (plain user label)
//   - "pod.operator.splunk.com/custom" (transformed during propagation)
//
// Examples of non-managed keys:
//   - "kubectl.kubernetes.io/last-applied-configuration" (kubectl internal)
//   - "operator.splunk.com/finalizer" (operator internal)
func IsManagedKey(key string, excludedPrefixes []string) bool {
	return !hasExcludedPrefix(key, excludedPrefixes)
}

// IsProtectedKey returns true if a key must be preserved and not removed during sync.
//
// A key is protected if it:
//   - Is part of the selector labels (used for pod selection by StatefulSet)
//   - Has an excluded prefix (operator-managed or system labels)
//
// Protected keys are NEVER removed during sync, even if they were previously managed.
// This prevents breaking StatefulSet pod selection which relies on immutable selectors.
//
// Note: In practice, selector labels shouldn't appear in managed key lists because
// they are set by the operator, not propagated from CR metadata. This is a safety check.
func IsProtectedKey(key string, selectorLabels map[string]string, excludedPrefixes []string) bool {
	// Key is protected if it's in selector labels
	if _, exists := selectorLabels[key]; exists {
		return true
	}
	// Key is protected if it has an excluded prefix
	return hasExcludedPrefix(key, excludedPrefixes)
}

// stripTargetPrefix removes the target prefix from a key if present.
// Returns the transformed key and true if transformation occurred,
// or the original key and false if no transformation was needed.
//
// This enables users to apply metadata selectively to specific resource types
// using a simple prefix-stripping pattern:
//
// Example transformations:
//   - "pod-only.prometheus.io/scrape" → "prometheus.io/scrape" (for Pod Template)
//   - "pod-only.istio.io/inject" → "istio.io/inject" (for Pod Template)
//   - "sts-only.amadeus.com/priority" → "amadeus.com/priority" (for StatefulSet)
//
// Use case: A user wants a label only on pods, not on the StatefulSet:
//
//	apiVersion: enterprise.splunk.com/v4
//	kind: Standalone
//	metadata:
//	  labels:
//	    pod-only.prometheus.io/scrape: "true"  # Only appears on pods as prometheus.io/scrape
func stripTargetPrefix(key, prefix string) (string, bool) {
	if strings.HasPrefix(key, prefix) {
		return key[len(prefix):], true
	}
	return key, false
}

// AppendParentMeta appends parent's metadata to a child (typically Pod Template).
// This function uses APPEND-ONLY semantics - it only adds keys that don't exist on the child.
//
// Behavior:
//   - Excludes labels/annotations with excluded prefixes (kubectl.kubernetes.io/*, operator.splunk.com/*, sts-only.*)
//   - Transforms pod-only.* keys by stripping the prefix (e.g., pod-only.prometheus.io/scrape → prometheus.io/scrape)
//   - Does NOT overwrite existing keys on child (no-clobber)
//   - Does NOT remove keys from child that are removed from parent
//
// Conflict Resolution:
//   - If both pod-only.XXXX/YYYY and explicit XXXX/YYYY exist on parent, the prefixed key wins (more specific)
//
// For full sync semantics (including key removal), use SyncParentMetaToPodTemplate instead.
//
// Deprecated: Use SyncParentMetaToPodTemplate for new code that needs sync semantics.
// This function is retained for backward compatibility and for cases where
// append-only behavior is explicitly desired.
func AppendParentMeta(child, parent metav1.Object) {
	// append labels from parent (excluding StatefulSet-only prefixes, transforming pod prefix)
	for k, v := range parent.GetLabels() {
		finalKey := k
		wasTransformed := false

		// Transform pod-only.* by stripping the prefix
		// Example: pod-only.prometheus.io/scrape → prometheus.io/scrape
		if newKey, transformed := stripTargetPrefix(k, podOnlyPrefix); transformed {
			finalKey = newKey
			wasTransformed = true
			// Conflict resolution: prefixed key wins over explicit key (more specific)
			// Don't skip - we'll set the value below, overwriting any explicit key
		}

		// Skip if child already has this key (no clobber) - but allow transformed keys to win
		if _, ok := child.GetLabels()[finalKey]; ok && !wasTransformed {
			continue
		}

		// For transformed keys, we intentionally propagate them
		// For non-transformed keys, apply standard exclusion logic
		if !wasTransformed && hasExcludedPrefix(k, podTemplateExcludedPrefixes) {
			continue
		}

		child.GetLabels()[finalKey] = v
	}

	// append annotations from parent (excluding StatefulSet-only prefixes, transforming pod prefix)
	for k, v := range parent.GetAnnotations() {
		finalKey := k
		wasTransformed := false

		// Transform pod-only.* by stripping the prefix
		// Example: pod-only.prometheus.io/scrape → prometheus.io/scrape
		if newKey, transformed := stripTargetPrefix(k, podOnlyPrefix); transformed {
			finalKey = newKey
			wasTransformed = true
			// Conflict resolution: prefixed key wins over explicit key (more specific)
			// Don't skip - we'll set the value below, overwriting any explicit key
		}

		// Skip if child already has this key (no clobber) - but allow transformed keys to win
		if _, ok := child.GetAnnotations()[finalKey]; ok && !wasTransformed {
			continue
		}

		// For transformed keys, we intentionally propagate them
		// For non-transformed keys, apply standard exclusion logic
		if !wasTransformed && hasExcludedPrefix(k, podTemplateExcludedPrefixes) {
			continue
		}

		child.GetAnnotations()[finalKey] = v
	}
}

// ComputeDesiredPodTemplateKeys calculates the labels and annotations from parent (CR)
// that are eligible for propagation to Pod Template.
// It applies prefix filtering (excludes kubectl.kubernetes.io/*, operator.splunk.com/*, sts-only.*)
// and prefix transformation (pod-only.* → prefix stripped, e.g., pod-only.prometheus.io/scrape → prometheus.io/scrape).
//
// Conflict Resolution:
//   - If both pod-only.XXXX/YYYY and explicit XXXX/YYYY exist on parent, the prefixed key wins (more specific)
//
// Returns maps of desired labels and annotations with transformed keys.
func ComputeDesiredPodTemplateKeys(parent metav1.Object) (labels map[string]string, annotations map[string]string) {
	labels = make(map[string]string)
	annotations = make(map[string]string)

	// Process labels - first pass: collect all non-prefixed keys
	for k, v := range parent.GetLabels() {
		// Skip keys with excluded prefixes
		if hasExcludedPrefix(k, podTemplateExcludedPrefixes) {
			continue
		}
		// Skip pod-only.* keys in first pass (handled in second pass)
		if strings.HasPrefix(k, podOnlyPrefix) {
			continue
		}
		labels[k] = v
	}

	// Process labels - second pass: add transformed pod-only.* keys (prefixed keys win as more specific)
	for k, v := range parent.GetLabels() {
		if newKey, transformed := stripTargetPrefix(k, podOnlyPrefix); transformed {
			// Conflict resolution: prefixed key wins over explicit key (more specific)
			labels[newKey] = v
		}
	}

	// Process annotations - first pass: collect all non-prefixed keys
	for k, v := range parent.GetAnnotations() {
		// Skip keys with excluded prefixes
		if hasExcludedPrefix(k, podTemplateExcludedPrefixes) {
			continue
		}
		// Skip pod-only.* keys in first pass (handled in second pass)
		if strings.HasPrefix(k, podOnlyPrefix) {
			continue
		}
		annotations[k] = v
	}

	// Process annotations - second pass: add transformed pod-only.* keys (prefixed keys win as more specific)
	for k, v := range parent.GetAnnotations() {
		if newKey, transformed := stripTargetPrefix(k, podOnlyPrefix); transformed {
			// Conflict resolution: prefixed key wins over explicit key (more specific)
			annotations[newKey] = v
		}
	}

	return labels, annotations
}

// SyncParentMeta synchronizes parent (CR) metadata to child (Pod Template) with full sync semantics.
// Unlike AppendParentMeta which only adds, this function also removes keys that were previously
// managed but no longer exist on the parent.
//
// Parameters:
//   - ctx: Context for logging
//   - child: The child object (Pod Template) whose metadata will be updated
//   - parent: The parent object (CR) that is the source of truth for metadata
//   - protectedLabels: Labels that must not be overwritten by parent metadata (typically selector labels).
//     These are labels set by the operator that must match the StatefulSet's immutable selector.
//     If a parent label key exists in protectedLabels, it will be skipped during propagation.
//   - previousManagedLabels: Keys that were previously propagated from CR (for removal detection)
//   - previousManagedAnnotations: Keys that were previously propagated from CR (for removal detection)
//
// Returns:
//   - newManagedLabels: Keys that are now managed (currently propagated from CR)
//   - newManagedAnnotations: Keys that are now managed (currently propagated from CR)
func SyncParentMetaToPodTemplate(ctx context.Context, child, parent metav1.Object, protectedLabels map[string]string, previousManagedLabels, previousManagedAnnotations []string) (newManagedLabels, newManagedAnnotations []string) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("SyncParentMetaToPodTemplate")

	// Compute desired keys from parent
	desiredLabels, desiredAnnotations := ComputeDesiredPodTemplateKeys(parent)

	// Initialize child maps if nil
	if child.GetLabels() == nil {
		child.SetLabels(make(map[string]string))
	}
	if child.GetAnnotations() == nil {
		child.SetAnnotations(make(map[string]string))
	}

	childLabels := child.GetLabels()
	childAnnotations := child.GetAnnotations()

	// Create sets for efficient lookup
	previousLabelSet := make(map[string]bool)
	for _, k := range previousManagedLabels {
		previousLabelSet[k] = true
	}
	previousAnnotationSet := make(map[string]bool)
	for _, k := range previousManagedAnnotations {
		previousAnnotationSet[k] = true
	}

	// Track changes for logging
	labelsAdded, labelsUpdated, labelsRemoved := 0, 0, 0
	annotationsAdded, annotationsUpdated, annotationsRemoved := 0, 0, 0

	// Sync labels: add/update desired keys, but skip protected labels
	for k, v := range desiredLabels {
		// Skip protected labels - these must not be overwritten by CR metadata
		// Protected labels are typically selector labels set by the operator
		if _, isProtected := protectedLabels[k]; isProtected {
			continue
		}
		if existing, exists := childLabels[k]; exists {
			if existing != v {
				labelsUpdated++
			}
		} else {
			labelsAdded++
		}
		childLabels[k] = v
	}

	// Sync labels: remove previously managed keys that are no longer desired
	// Skip protected labels - they must never be removed even if they were in previousManagedLabels
	for _, k := range previousManagedLabels {
		if _, isProtected := protectedLabels[k]; isProtected {
			continue
		}
		if _, stillDesired := desiredLabels[k]; !stillDesired {
			delete(childLabels, k)
			labelsRemoved++
		}
	}

	// Sync annotations: add/update desired keys
	for k, v := range desiredAnnotations {
		if existing, exists := childAnnotations[k]; exists {
			if existing != v {
				annotationsUpdated++
			}
		} else {
			annotationsAdded++
		}
		childAnnotations[k] = v
	}

	// Sync annotations: remove previously managed keys that are no longer desired
	for _, k := range previousManagedAnnotations {
		if _, stillDesired := desiredAnnotations[k]; !stillDesired {
			delete(childAnnotations, k)
			annotationsRemoved++
		}
	}

	// Build list of currently managed keys (excluding protected labels)
	newManagedLabels = make([]string, 0, len(desiredLabels))
	for k := range desiredLabels {
		// Don't track protected labels as "managed" since we didn't propagate them
		if _, isProtected := protectedLabels[k]; isProtected {
			continue
		}
		newManagedLabels = append(newManagedLabels, k)
	}
	sort.Strings(newManagedLabels)

	newManagedAnnotations = make([]string, 0, len(desiredAnnotations))
	for k := range desiredAnnotations {
		newManagedAnnotations = append(newManagedAnnotations, k)
	}
	sort.Strings(newManagedAnnotations)

	// Log summary of changes (Info for removals, Debug for adds/updates)
	if labelsRemoved > 0 || annotationsRemoved > 0 {
		scopedLog.Info("Pod template metadata sync removed keys",
			"labelsRemoved", labelsRemoved,
			"annotationsRemoved", annotationsRemoved)
	}
	if labelsAdded > 0 || labelsUpdated > 0 || annotationsAdded > 0 || annotationsUpdated > 0 {
		scopedLog.V(1).Info("Pod template metadata sync added/updated keys",
			"labelsAdded", labelsAdded,
			"labelsUpdated", labelsUpdated,
			"annotationsAdded", annotationsAdded,
			"annotationsUpdated", annotationsUpdated)
	}

	return newManagedLabels, newManagedAnnotations
}

// ComputeDesiredStatefulSetKeys calculates the labels and annotations from parent (CR)
// that are eligible for propagation to StatefulSet ObjectMeta.
// It applies prefix filtering (excludes kubectl.kubernetes.io/*, operator.splunk.com/*, pod-only.*)
// and prefix transformation (sts-only.* → prefix stripped, e.g., sts-only.amadeus.com/priority → amadeus.com/priority).
//
// Conflict Resolution:
//   - If both sts-only.XXXX/YYYY and explicit XXXX/YYYY exist on parent, the prefixed key wins (more specific)
//
// Returns maps of desired labels and annotations with transformed keys.
func ComputeDesiredStatefulSetKeys(parent metav1.Object) (labels map[string]string, annotations map[string]string) {
	labels = make(map[string]string)
	annotations = make(map[string]string)

	// Process labels - first pass: collect all non-prefixed keys
	for k, v := range parent.GetLabels() {
		// Skip keys with excluded prefixes
		if hasExcludedPrefix(k, statefulSetExcludedPrefixes) {
			continue
		}
		// Skip sts-only.* keys in first pass (handled in second pass)
		if strings.HasPrefix(k, stsOnlyPrefix) {
			continue
		}
		labels[k] = v
	}

	// Process labels - second pass: add transformed sts-only.* keys (prefixed keys win as more specific)
	for k, v := range parent.GetLabels() {
		if newKey, transformed := stripTargetPrefix(k, stsOnlyPrefix); transformed {
			// Conflict resolution: prefixed key wins over explicit key (more specific)
			labels[newKey] = v
		}
	}

	// Process annotations - first pass: collect all non-prefixed keys
	for k, v := range parent.GetAnnotations() {
		// Skip keys with excluded prefixes
		if hasExcludedPrefix(k, statefulSetExcludedPrefixes) {
			continue
		}
		// Skip sts-only.* keys in first pass (handled in second pass)
		if strings.HasPrefix(k, stsOnlyPrefix) {
			continue
		}
		annotations[k] = v
	}

	// Process annotations - second pass: add transformed sts-only.* keys (prefixed keys win as more specific)
	for k, v := range parent.GetAnnotations() {
		if newKey, transformed := stripTargetPrefix(k, stsOnlyPrefix); transformed {
			// Conflict resolution: prefixed key wins over explicit key (more specific)
			annotations[newKey] = v
		}
	}

	return labels, annotations
}

// SyncParentMetaToStatefulSet synchronizes parent (CR) metadata to StatefulSet ObjectMeta with full sync semantics.
// Unlike AppendParentMetaToStatefulSet which only adds, this function also removes keys that were previously
// managed but no longer exist on the parent.
//
// Parameters:
//   - ctx: Context for logging
//   - child: The StatefulSet whose metadata will be updated
//   - parent: The parent object (CR) that is the source of truth for metadata
//   - selectorLabels: Labels used for pod selection that must never be removed
//
// This function:
//   - Reads previous managed keys from child's annotations (using GetManagedLabelKeys/GetManagedAnnotationKeys)
//   - Computes desired keys from parent using ComputeDesiredStatefulSetKeys
//   - Adds/updates desired keys
//   - Removes keys that are in previousManaged but not in desired (respecting protected keys)
//   - Updates managed key tracking annotations (using SetManagedLabelKeys/SetManagedAnnotationKeys)
func SyncParentMetaToStatefulSet(ctx context.Context, child, parent metav1.Object, selectorLabels map[string]string) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("SyncParentMetaToStatefulSet").WithValues(
		"namespace", child.GetNamespace(),
		"name", child.GetName())

	// Initialize child maps if nil
	if child.GetLabels() == nil {
		child.SetLabels(make(map[string]string))
	}
	if child.GetAnnotations() == nil {
		child.SetAnnotations(make(map[string]string))
	}

	childLabels := child.GetLabels()
	childAnnotations := child.GetAnnotations()

	// Read previous managed keys from child's annotations
	previousManagedLabels := GetManagedLabelKeys(childAnnotations)
	previousManagedAnnotations := GetManagedAnnotationKeys(childAnnotations)

	// Compute desired keys from parent
	desiredLabels, desiredAnnotations := ComputeDesiredStatefulSetKeys(parent)

	// Track changes for logging
	labelsAdded, labelsUpdated, labelsRemoved := 0, 0, 0
	annotationsAdded, annotationsUpdated, annotationsRemoved := 0, 0, 0

	// Sync labels: add/update desired keys
	for k, v := range desiredLabels {
		if existing, exists := childLabels[k]; exists {
			if existing != v {
				labelsUpdated++
			}
		} else {
			labelsAdded++
		}
		childLabels[k] = v
	}

	// Sync labels: remove previously managed keys that are no longer desired
	// Only protect selector labels - managed keys are removable even with excluded prefixes
	// because they were put there by us from CR metadata (possibly transformed)
	for _, k := range previousManagedLabels {
		if _, stillDesired := desiredLabels[k]; !stillDesired {
			// Only protect selector labels - they must never be removed
			if _, isSelectorLabel := selectorLabels[k]; !isSelectorLabel {
				delete(childLabels, k)
				labelsRemoved++
			}
		}
	}

	// Sync annotations: add/update desired keys
	for k, v := range desiredAnnotations {
		if existing, exists := childAnnotations[k]; exists {
			if existing != v {
				annotationsUpdated++
			}
		} else {
			annotationsAdded++
		}
		childAnnotations[k] = v
	}

	// Sync annotations: remove previously managed keys that are no longer desired
	// Annotations don't have selector label concerns, so all managed keys are removable
	for _, k := range previousManagedAnnotations {
		if _, stillDesired := desiredAnnotations[k]; !stillDesired {
			delete(childAnnotations, k)
			annotationsRemoved++
		}
	}

	// Build list of currently managed keys
	newManagedLabels := make([]string, 0, len(desiredLabels))
	for k := range desiredLabels {
		newManagedLabels = append(newManagedLabels, k)
	}

	newManagedAnnotations := make([]string, 0, len(desiredAnnotations))
	for k := range desiredAnnotations {
		newManagedAnnotations = append(newManagedAnnotations, k)
	}

	// Update managed key tracking annotations
	SetManagedLabelKeys(childAnnotations, newManagedLabels)
	SetManagedAnnotationKeys(childAnnotations, newManagedAnnotations)

	// Log summary of changes (Info for removals, Debug for adds/updates)
	if labelsRemoved > 0 || annotationsRemoved > 0 {
		scopedLog.Info("StatefulSet metadata sync removed keys",
			"labelsRemoved", labelsRemoved,
			"annotationsRemoved", annotationsRemoved)
	}
	if labelsAdded > 0 || labelsUpdated > 0 || annotationsAdded > 0 || annotationsUpdated > 0 {
		scopedLog.V(1).Info("StatefulSet metadata sync added/updated keys",
			"labelsAdded", labelsAdded,
			"labelsUpdated", labelsUpdated,
			"annotationsAdded", annotationsAdded,
			"annotationsUpdated", annotationsUpdated)
	}
}

// ParseResourceQuantity parses and returns a resource quantity from a string.
func ParseResourceQuantity(str string, useIfEmpty string) (resource.Quantity, error) {
	var result resource.Quantity

	if str == "" {
		if useIfEmpty != "" {
			result = resource.MustParse(useIfEmpty)
		}
	} else {
		var err error
		result, err = resource.ParseQuantity(str)
		if err != nil {
			return result, fmt.Errorf("invalid resource quantity \"%s\": %s", str, err)
		}
	}

	return result, nil
}

// GetServiceFQDN returns the fully qualified domain name for a Kubernetes service.
func GetServiceFQDN(namespace string, name string) string {
	clusterDomain := os.Getenv("CLUSTER_DOMAIN")
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}
	return fmt.Sprintf(
		"%s.%s.svc.%s",
		name, namespace, clusterDomain,
	)
}

// GenerateSecret returns a randomly generated sequence of text that is n bytes in length.
func GenerateSecret(SecretBytes string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = SecretBytes[rand.Int63()%int64(len(SecretBytes))]
	}
	return b
}

// SortContainerPorts returns a sorted list of Kubernetes ContainerPorts.
func SortContainerPorts(ports []corev1.ContainerPort) []corev1.ContainerPort {
	sorted := make([]corev1.ContainerPort, len(ports))
	copy(sorted, ports)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ContainerPort < sorted[j].ContainerPort })
	return sorted
}

// SortServicePorts returns a sorted list of Kubernetes ServicePorts.
func SortServicePorts(ports []corev1.ServicePort) []corev1.ServicePort {
	sorted := make([]corev1.ServicePort, len(ports))
	copy(sorted, ports)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Port < sorted[j].Port })
	return sorted
}

// CompareContainerPorts is a generic comparer of two Kubernetes ContainerPorts.
// It returns true if there are material differences between them, or false otherwise.
// TODO: could use refactoring; lots of boilerplate copy-pasta here
func CompareContainerPorts(a []corev1.ContainerPort, b []corev1.ContainerPort) bool {
	return sortAndCompareSlices(a, b, SortFieldContainerPort)
}

// CompareServicePorts is a generic comparer of two Kubernetes ServicePorts.
// It returns true if there are material differences between them, or false otherwise.
// TODO: could use refactoring; lots of boilerplate copy-pasta here
func CompareServicePorts(a []corev1.ServicePort, b []corev1.ServicePort) bool {
	return sortAndCompareSlices(a, b, SortFieldPort)
}

// CompareEnvs is a generic comparer of two Kubernetes Env variables.
// It returns true if there are material differences between them, or false otherwise.
func CompareEnvs(a []corev1.EnvVar, b []corev1.EnvVar) bool {
	return sortAndCompareSlices(a, b, SortFieldName)
}

// CompareTolerations compares the 2 list of tolerations
func CompareTolerations(a []corev1.Toleration, b []corev1.Toleration) bool {
	return sortAndCompareSlices(a, b, SortFieldKey)
}

// CompareTopologySpreadConstraints compares the 2 list of topologySpreadConstraints
func CompareTopologySpreadConstraints(a []corev1.TopologySpreadConstraint, b []corev1.TopologySpreadConstraint) bool {
	return sortAndCompareSlices(a, b, SortFieldTopologyKey)
}

// CompareImagePullSecrets compares the 2 list of imagePullSecrets
func CompareImagePullSecrets(a []corev1.LocalObjectReference, b []corev1.LocalObjectReference) bool {
	return sortAndCompareSlices(a, b, SortFieldName)
}

// CompareVolumes is a generic comparer of two Kubernetes Volumes.
// It returns true if there are material differences between them, or false otherwise.
func CompareVolumes(a []corev1.Volume, b []corev1.Volume) bool {
	return sortAndCompareSlices(a, b, SortFieldName)
}

// CompareVolumeMounts is a generic comparer of two Kubernetes VolumeMounts.
// It returns true if there are material differences between them, or false otherwise.
func CompareVolumeMounts(a []corev1.VolumeMount, b []corev1.VolumeMount) bool {
	return sortAndCompareSlices(a, b, SortFieldName)
}

// CompareByMarshall compares two Kubernetes objects by marshalling them to JSON.
// It returns true if there are differences between the two marshalled values, or false otherwise.
func CompareByMarshall(a interface{}, b interface{}) bool {
	aBytes, err := json.Marshal(a)
	if err != nil {
		return true
	}

	bBytes, err := json.Marshal(b)
	if err != nil {
		return true
	}

	if !bytes.Equal(aBytes, bBytes) {
		return true
	}

	return false
}

// CompareSortedStrings returns true if there are differences between the two sorted lists of strings, or false otherwise.
func CompareSortedStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return true
	}

	sort.Strings(a)
	sort.Strings(b)

	return !reflect.DeepEqual(a, b)
}

// GetIstioAnnotations returns a map of istio annotations for a pod template
func GetIstioAnnotations(ports []corev1.ContainerPort) map[string]string {
	// list of ports within the deployments that we want istio to leave alone
	excludeOutboundPorts := []int32{8089, 8191, 9997}

	// calculate outbound port exclusions
	excludeOutboundPortsLookup := make(map[int32]bool)
	excludeOutboundPortsBuf := bytes.NewBufferString("")
	for idx := range excludeOutboundPorts {
		if excludeOutboundPortsBuf.Len() > 0 {
			fmt.Fprint(excludeOutboundPortsBuf, ",")
		}
		fmt.Fprintf(excludeOutboundPortsBuf, "%d", excludeOutboundPorts[idx])
		excludeOutboundPortsLookup[excludeOutboundPorts[idx]] = true
	}

	// calculate inbound port inclusions
	includeInboundPortsBuf := bytes.NewBufferString("")
	sortedPorts := SortContainerPorts(ports)
	for idx := range sortedPorts {
		_, skip := excludeOutboundPortsLookup[sortedPorts[idx].ContainerPort]
		if !skip {
			if includeInboundPortsBuf.Len() > 0 {
				fmt.Fprint(includeInboundPortsBuf, ",")
			}
			fmt.Fprintf(includeInboundPortsBuf, "%d", sortedPorts[idx].ContainerPort)
		}
	}

	return map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": excludeOutboundPortsBuf.String(),
		"traffic.sidecar.istio.io/includeInboundPorts":  includeInboundPortsBuf.String(),
	}
}

// GetLabels returns a map of labels to use for managed components.
func GetLabels(component, name, instanceIdentifier string, partOfIdentifier string, selectFew []string) (map[string]string, error) {
	var err error = nil
	labels := make(map[string]string)
	labelTypeMap := GetLabelTypes()
	if len(selectFew) == 0 {
		// see https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels
		labels[labelTypeMap["manager"]] = "splunk-operator"
		labels[labelTypeMap["component"]] = component
		labels[labelTypeMap["name"]] = name
		labels[labelTypeMap["partof"]] = fmt.Sprintf("splunk-%s-%s", partOfIdentifier, component)

		if len(instanceIdentifier) > 0 {
			labels[labelTypeMap["instance"]] = fmt.Sprintf("splunk-%s-%s", instanceIdentifier, name)
		}
	} else {
		for _, s := range selectFew {
			switch s {
			case "manager":
				labels[labelTypeMap["manager"]] = "splunk-operator"
			case "component":
				labels[labelTypeMap["component"]] = component
			case "name":
				labels[labelTypeMap["name"]] = name
			case "partof":
				labels[labelTypeMap["partof"]] = fmt.Sprintf("splunk-%s-%s", partOfIdentifier, component)
			case "instance":
				if len(instanceIdentifier) > 0 {
					labels[labelTypeMap["instance"]] = fmt.Sprintf("splunk-%s-%s", instanceIdentifier, name)
				}
			default:
				err = fmt.Errorf("Incorrect label type %s", s)
			}
		}
	}

	return labels, err
}

// AppendPodAntiAffinity appends a Kubernetes Affinity object to include anti-affinity for pods of the same type, and returns the result.
func AppendPodAntiAffinity(affinity *corev1.Affinity, identifier string, typeLabel string) *corev1.Affinity {
	if affinity == nil {
		affinity = &corev1.Affinity{}
	} else {
		affinity = affinity.DeepCopy()
	}

	if affinity.PodAntiAffinity == nil {
		affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
	}

	// prepare match expressions to match select labels
	matchExpressions := []metav1.LabelSelectorRequirement{
		{
			Key:      "app.kubernetes.io/instance",
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{fmt.Sprintf("splunk-%s-%s", identifier, typeLabel)},
		},
	}

	affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
		affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
		corev1.WeightedPodAffinityTerm{
			Weight: 100,
			PodAffinityTerm: corev1.PodAffinityTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: matchExpressions,
				},
				TopologyKey: "kubernetes.io/hostname",
			},
		},
	)

	return affinity
}

// sortAndCompareSlices sorts and compare the slices for equality. Return true if NOT equal. False otherwise
func sortAndCompareSlices(a interface{}, b interface{}, keyName string) bool {
	aType := reflect.TypeOf(a)
	bType := reflect.TypeOf(b)

	if aType.Kind() != reflect.Slice || bType.Kind() != reflect.Slice {
		panic(fmt.Sprintf("SortAndCompareSlices can only be used on slices: Kind(a)=%v, Kind(b)=%v", aType.Kind(), bType.Kind()))
	}

	if aType.Elem() != bType.Elem() {
		panic(fmt.Sprintf("SortAndCompareSlides can only be used on slices on the same type: Elem(a)=%v, Elem(b)=%v", aType.Elem(), bType.Elem()))
	}

	_, found := aType.Elem().FieldByName(keyName)
	if !found {
		panic(fmt.Sprintf("SortAndCompareSlides cannot find the specified key name '%s' to sort on", keyName))
	}

	aValue := reflect.ValueOf(a)
	bValue := reflect.ValueOf(b)
	if aValue.Len() != bValue.Len() {
		return true
	}

	// Sort slices
	SortSlice(a, keyName)
	SortSlice(b, keyName)

	return !reflect.DeepEqual(a, b)
}

// SortSlice sorts a slice of any kind by keyName
func SortSlice(a interface{}, keyName string) {
	aType := reflect.TypeOf(a)

	if aType.Kind() != reflect.Slice {
		panic(fmt.Sprintf("SortSlice can only be used on slices: Kind(a)=%v", aType.Kind()))
	}

	sortFunc := func(s interface{}, i, j int) bool {
		sValue := reflect.ValueOf(s)

		val1 := sValue.Index(i).FieldByName(keyName)
		val2 := sValue.Index(j).FieldByName(keyName)

		switch val1.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return val1.Int() < val2.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return val1.Uint() < val2.Uint()
		case reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
			return val1.Float() < val2.Float()
		case reflect.String:
			return val1.String() < val2.String()
		default:
			panic(fmt.Sprintf("SortAndCompareSlides can only sort on keyName of int, uint, float or string type: Kind(%s)=%v", keyName, val1.Kind()))
		}
	}

	sort.Slice(a, func(i, j int) bool {
		return sortFunc(a, i, j)
	})
}
