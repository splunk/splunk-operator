// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

// Package certmanager is a CRD-decoupled library for generating certificates via
// cert-manager, issued by an admin-provided Issuer or ClusterIssuer. It has
// no dependency on any SOK-specific CR type — callers pass a Kubernetes
// client, target namespace, secret name, and an IssuerRef, and the library
// verifies the referenced Issuer/ClusterIssuer exists before creating the
// cert-manager Certificate CR. The library never creates an Issuer or
// ClusterIssuer itself.
//
// This package is distinct from pkg/splunk/workflow/certs, which handles
// Phase 1 mount-only reconciliation (validating, mounting, and rotating
// already-existing cert secrets into pod templates). This package is the
// Phase 2 auto-generation layer that workflow/certs calls into when a
// referenced secret does not yet exist.
package certmanager
