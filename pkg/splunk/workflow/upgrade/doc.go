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
Package upgrade contains the shared upgrade-path validation used by the
Enterprise CR implementations.

The current implementation retains its existing CRD and Kubernetes-client
inputs to avoid changing upgrade behavior. TODO: once all CRs have migrated
from enterprise, revisit this boundary and split it into pure decision logic
plus caller-owned Kubernetes orchestration if needed.
*/
package upgrade
