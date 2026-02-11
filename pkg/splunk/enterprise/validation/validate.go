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
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

var (
	scheme = runtime.NewScheme()
	codecs serializer.CodecFactory
)

func init() {
	_ = enterpriseApi.AddToScheme(scheme)
	codecs = serializer.NewCodecFactory(scheme)
}

// Validate performs validation on an AdmissionReview request
// Returns warnings (even on success) and an error if validation fails
func Validate(ar *admissionv1.AdmissionReview, validators map[schema.GroupVersionResource]Validator) ([]string, error) {
	if ar == nil || ar.Request == nil {
		return nil, fmt.Errorf("admission review or request is nil")
	}

	req := ar.Request

	// Extract GVR from request
	gvr := schema.GroupVersionResource{
		Group:    req.Resource.Group,
		Version:  req.Resource.Version,
		Resource: req.Resource.Resource,
	}

	// Lookup validator by GVR
	validator, ok := validators[gvr]
	if !ok {
		return nil, fmt.Errorf("no validator registered for resource %s", gvr.String())
	}

	// Deserialize the object
	obj, err := deserializeObject(req.Object.Raw)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize object: %w", err)
	}

	// Deserialize old object if present (for UPDATE operations)
	var oldObj runtime.Object
	if len(req.OldObject.Raw) > 0 {
		oldObj, err = deserializeObject(req.OldObject.Raw)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize old object: %w", err)
		}
	}

	var fieldErrs field.ErrorList
	var warnings []string

	// Perform validation based on operation
	switch req.Operation {
	case admissionv1.Create:
		fieldErrs = validator.ValidateCreate(obj)
		warnings = validator.GetWarningsOnCreate(obj)

	case admissionv1.Update:
		fieldErrs = validator.ValidateUpdate(obj, oldObj)
		warnings = validator.GetWarningsOnUpdate(obj, oldObj)

	default:
		// For other operations (DELETE, CONNECT), allow by default
		return nil, nil
	}

	// If there are validation errors, return an aggregate error
	if len(fieldErrs) > 0 {
		groupKind := validator.GetGroupKind(obj)
		name := validator.GetName(obj)
		return warnings, apierrors.NewInvalid(groupKind, name, fieldErrs)
	}

	return warnings, nil
}

// deserializeObject deserializes raw bytes into a runtime.Object
func deserializeObject(raw []byte) (runtime.Object, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty raw object")
	}

	decoder := codecs.UniversalDeserializer()
	obj, _, err := decoder.Decode(raw, nil, nil)
	if err != nil {
		return nil, err
	}

	return obj, nil
}
