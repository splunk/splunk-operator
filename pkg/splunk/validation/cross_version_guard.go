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

package validation

import (
	"context"
	"encoding/json"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApiV4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// crossVersionPolicy says how to tell whether a write through an older served
// version would drop fields that only the stored version can represent.
type crossVersionPolicy struct {
	kind            string
	newStored       func() client.Object
	unrepresentable func(stored client.Object) []string
}

// crossVersionPolicies is keyed by the older served version a write arrives at.
var crossVersionPolicies = map[schema.GroupVersionResource]crossVersionPolicy{
	IndexerClusterV3GVR: {
		kind: "IndexerCluster",
		newStored: func() client.Object {
			return new(enterpriseApiV4.IndexerCluster{})
		},
		unrepresentable: func(stored client.Object) []string {
			return unrepresentableIndexerClusterFields(&stored.(*enterpriseApiV4.IndexerCluster).Spec)
		},
	},
	SearchHeadClusterV3GVR: {
		kind: "SearchHeadCluster",
		newStored: func() client.Object {
			return new(enterpriseApiV4.SearchHeadCluster{})
		},
		unrepresentable: func(stored client.Object) []string {
			return unrepresentableSearchHeadClusterFields(&stored.(*enterpriseApiV4.SearchHeadCluster).Spec)
		},
	},
}

// isCrossVersionGuarded reports whether gvr is an older served version that
// only the cross-version guard handles.
func isCrossVersionGuarded(gvr schema.GroupVersionResource) bool {
	_, ok := crossVersionPolicies[gvr]
	return ok
}

// CrossVersionFieldGuard denies writes through an older served version of a
// CRD when the stored object has fields that version cannot represent.
type CrossVersionFieldGuard struct {
	reader client.Reader
}

// NewCrossVersionFieldGuard returns a guard that reads stored objects through
// reader, which must not be cached: a stale read would let a write through.
func NewCrossVersionFieldGuard(reader client.Reader) *CrossVersionFieldGuard {
	return &CrossVersionFieldGuard{reader: reader}
}

// Validate returns an error denying req, or nil to allow it. The policy is chosen
// by the resource the client requested rather than req.Kind, because a /scale
// request arrives as autoscaling/v1 Scale.
func (g *CrossVersionFieldGuard) Validate(ctx context.Context, req *admissionv1.AdmissionRequest) error {
	// An Equivalent webhook entry gets v3 writes relabelled to v4; RequestResource keeps the client's version.
	r := req.Resource
	if req.RequestResource != nil {
		r = *req.RequestResource
	}
	requested := schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}

	policy, ok := crossVersionPolicies[requested]
	if !ok || (req.Operation != admissionv1.Update && req.Operation != admissionv1.Delete) {
		return nil
	}

	var old metav1.PartialObjectMetadata
	if err := json.Unmarshal(req.OldObject.Raw, &old); err != nil {
		return apierrors.NewBadRequest(fmt.Sprintf("decode oldObject: %v", err))
	}

	if req.Operation == admissionv1.Delete {
		if writesBack, err := deleteWritesBack(req, old.Finalizers); !writesBack || err != nil {
			if err != nil {
				return err
			}

			return nil
		}
	}

	// deletecollection sends each item with an empty req.Name.
	key := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}
	if key.Name == "" {
		key = types.NamespacedName{Namespace: old.Namespace, Name: old.Name}
	}

	stored := policy.newStored()
	if err := g.reader.Get(ctx, key, stored); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return apierrors.NewInternalError(fmt.Errorf("read %s %s: %w", policy.kind, key, err))
	}

	if stored.GetResourceVersion() != old.ResourceVersion {
		return apierrors.NewConflict(requested.GroupResource(), key.Name, fmt.Errorf(
			"the object has been modified; please apply your changes to the latest version and try again"))
	}

	return refuseIfUnrepresentable(policy.kind, key.Namespace, key.Name, policy.unrepresentable(stored))
}

// deleteWritesBack reports whether a DELETE updates the stored object instead
// of removing it: finalizers, and foreground or orphan propagation, all leave
// the object in place with deletion metadata written back at the request's
// version.
func deleteWritesBack(req *admissionv1.AdmissionRequest, finalizers []string) (bool, error) {
	if req.SubResource != "" {
		return false, nil
	}
	if len(finalizers) > 0 {
		return true, nil
	}
	if len(req.Options.Raw) == 0 {
		return false, nil
	}

	var opts metav1.DeleteOptions
	if err := json.Unmarshal(req.Options.Raw, &opts); err != nil {
		return false, apierrors.NewBadRequest(fmt.Sprintf("decode delete options: %v", err))
	}
	if opts.PropagationPolicy != nil && (*opts.PropagationPolicy == metav1.DeletePropagationForeground || *opts.PropagationPolicy == metav1.DeletePropagationOrphan) {
		return true, nil
	}

	// orphanDependents is deprecated in the Go type, but the API server still honours it so it's included
	return opts.OrphanDependents != nil && *opts.OrphanDependents, nil
}
