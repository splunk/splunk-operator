/*
Copyright 2026.

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
package core

import "errors"

var (
	errContractsNotReady    = errors.New("contracts not ready")
	errServerTLSLeafInvalid = errors.New("server TLS secret contains invalid certificate material")
)

type reconcileFailure struct {
	reason conditionReasons
	err    error
}

func newReconcileFailure(reason conditionReasons, err error) *reconcileFailure {
	return &reconcileFailure{reason: reason, err: err}
}

func (e *reconcileFailure) Error() string { return e.err.Error() }
func (e *reconcileFailure) Unwrap() error { return e.err }
