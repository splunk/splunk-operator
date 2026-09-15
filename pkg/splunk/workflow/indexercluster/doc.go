// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

/*
Package indexercluster implements multi-step IndexerCluster workflows: peer
incarnation, decommission, rebalance, scaling, and secret synchronization.

This is intentionally a domain-specific workflow package. It accepts the
IndexerCluster API type and owns only the state transitions and Splunk API
operations required by the pod-manager contract; reconcile/indexercluster owns
Kubernetes object application, status persistence, and request orchestration.

// TODO: Once all CRs have migrated from enterprise, revisit this boundary and
// make the workflow CR-agnostic if needed.
*/
package indexercluster
