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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidatableObject is the interface that CRD types must implement to be validated
type ValidatableObject interface {
	runtime.Object
	GetName() string
	GetNamespace() string
	GetObjectKind() schema.ObjectKind
}

// Validator is the interface for validating Kubernetes objects
type Validator interface {
	// ValidateCreate validates an object on CREATE operation
	ValidateCreate(obj runtime.Object) field.ErrorList

	// ValidateUpdate validates an object on UPDATE operation
	ValidateUpdate(obj, oldObj runtime.Object) field.ErrorList

	// GetGroupKind returns the GroupKind for the object
	GetGroupKind(obj runtime.Object) schema.GroupKind

	// GetName returns the name of the object
	GetName(obj runtime.Object) string

	// GetWarningsOnCreate returns warnings for CREATE operation
	GetWarningsOnCreate(obj runtime.Object) []string

	// GetWarningsOnUpdate returns warnings for UPDATE operation
	GetWarningsOnUpdate(obj, oldObj runtime.Object) []string
}

// GenericValidator is a type-safe wrapper for validating specific CRD types
type GenericValidator[T ValidatableObject] struct {
	// ValidateCreateFunc is the function to validate on CREATE
	ValidateCreateFunc func(obj T) field.ErrorList

	// ValidateUpdateFunc is the function to validate on UPDATE
	ValidateUpdateFunc func(obj, oldObj T) field.ErrorList

	// WarningsOnCreateFunc returns warnings on CREATE
	WarningsOnCreateFunc func(obj T) []string

	// WarningsOnUpdateFunc returns warnings on UPDATE
	WarningsOnUpdateFunc func(obj, oldObj T) []string

	// GroupKind is the GroupKind for this validator
	GroupKind schema.GroupKind
}

// ValidateCreate implements Validator interface
func (v *GenericValidator[T]) ValidateCreate(obj runtime.Object) field.ErrorList {
	if v.ValidateCreateFunc == nil {
		return nil
	}
	typedObj, ok := obj.(T)
	if !ok {
		return field.ErrorList{field.InternalError(nil,
			&TypeAssertionError{Expected: new(T), Actual: obj})}
	}
	return v.ValidateCreateFunc(typedObj)
}

// ValidateUpdate implements Validator interface
func (v *GenericValidator[T]) ValidateUpdate(obj, oldObj runtime.Object) field.ErrorList {
	if v.ValidateUpdateFunc == nil {
		return nil
	}
	typedObj, ok := obj.(T)
	if !ok {
		return field.ErrorList{field.InternalError(nil,
			&TypeAssertionError{Expected: new(T), Actual: obj})}
	}
	typedOldObj, ok := oldObj.(T)
	if !ok {
		return field.ErrorList{field.InternalError(nil,
			&TypeAssertionError{Expected: new(T), Actual: oldObj})}
	}
	return v.ValidateUpdateFunc(typedObj, typedOldObj)
}

// GetGroupKind implements Validator interface
func (v *GenericValidator[T]) GetGroupKind(obj runtime.Object) schema.GroupKind {
	return v.GroupKind
}

// GetName implements Validator interface
func (v *GenericValidator[T]) GetName(obj runtime.Object) string {
	typedObj, ok := obj.(T)
	if !ok {
		return ""
	}
	return typedObj.GetName()
}

// GetWarningsOnCreate implements Validator interface
func (v *GenericValidator[T]) GetWarningsOnCreate(obj runtime.Object) []string {
	if v.WarningsOnCreateFunc == nil {
		return nil
	}
	typedObj, ok := obj.(T)
	if !ok {
		return nil
	}
	return v.WarningsOnCreateFunc(typedObj)
}

// GetWarningsOnUpdate implements Validator interface
func (v *GenericValidator[T]) GetWarningsOnUpdate(obj, oldObj runtime.Object) []string {
	if v.WarningsOnUpdateFunc == nil {
		return nil
	}
	typedObj, ok := obj.(T)
	if !ok {
		return nil
	}
	typedOldObj, ok := oldObj.(T)
	if !ok {
		return nil
	}
	return v.WarningsOnUpdateFunc(typedObj, typedOldObj)
}

// Ensure GenericValidator implements Validator
var _ Validator = &GenericValidator[*dummyObject]{}

// dummyObject is used only for compile-time interface check
type dummyObject struct{}

func (d *dummyObject) GetName() string                  { return "" }
func (d *dummyObject) GetNamespace() string             { return "" }
func (d *dummyObject) GetObjectKind() schema.ObjectKind { return nil }
func (d *dummyObject) DeepCopyObject() runtime.Object   { return d }

// TypeAssertionError is returned when type assertion fails
type TypeAssertionError struct {
	Expected interface{}
	Actual   interface{}
}

func (e *TypeAssertionError) Error() string {
	return "type assertion failed"
}
