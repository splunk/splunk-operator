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

// Package noah is the narrow, workload-neutral domain boundary for Splunk
// workloads that connect to Noah.
//
// The package owns semantics that are genuinely shared by more than one
// workload: resolving and validating a same-namespace NoahCluster and its
// credential Secret, constructing an authenticated client without making a
// network request, and exposing safe derived connection values. It may also
// own pure Noah-domain rules when concrete IndexerCluster and
// SearchHeadCluster consumers require identical behavior.
//
// A resolved connection can expose a defensive copy of the credential for the
// consumers that must build Secret-backed workload configuration. That value
// remains sensitive: callers must not log it or write it to ConfigMaps, status,
// events, or command arguments.
//
// Package noah is not a general service layer. HTTP and HMAC protocol details
// remain in client/noah. Indexer and search-head lifecycle policy remains in
// their workflow packages. Kubernetes object construction remains in
// resources, and reconcilers continue to own Kubernetes writes, status,
// conditions, events, errors, and requeue decisions. In particular, this
// package must not wrap Noah operations merely to centralize client calls.
//
// The package may read NoahCluster and Secret objects while resolving a
// connection, but resolution must not mutate Kubernetes resources or contact
// Noah. Allowed imports from pkg/splunk are common, util, and client/noah.
package noah
