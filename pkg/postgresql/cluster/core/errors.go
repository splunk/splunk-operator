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

	// errSweepConnect marks a transient post-recovery sweep failure: the restored DB is not
	// reachable yet (still initialising). The component stays Provisioning and requeues to retry.
	errSweepConnect = errors.New("post-recovery credential sweep cannot connect yet")
	// errSweepTerminal marks a sweep failure that retrying will not fix — either a terminal
	// connect failure (bad credentials, insufficient privilege) or a failure disabling roles.
	// The component surfaces Failed rather than requeuing.
	errSweepTerminal = errors.New("post-recovery credential sweep failed")
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

// secretReconcileError is the single typed, terminal failure raised while
// reconciling an externally managed superuser secret — covering both "absent"
// (reasonExternalSecretMissing) and "present but invalid" (empty/missing data,
// missing required keys, invalid username, missing reload label). Every case is
// deterministic, so Observe terminalizes on this type rather than retrying; only
// transient failures are returned untyped. It carries the conditionReason so the
// observe step maps it directly onto the secret's health without re-deriving the
// cause, and downstream callers branch on reason rather than on a distinct type.
// message holds only user-facing context.
type secretReconcileError struct {
	message string
	reason  conditionReasons
}

func (e secretReconcileError) Error() string { return e.message }
