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
Package workflow contains multi-step, stateful operations that are CR-agnostic.
Each sub-package owns a domain workflow (App Framework sync, rolling upgrade,
SHC captain election, indexer decommission, etc.) and is consumed by one or more
reconcile/<cr>/ packages.

Workflow packages call client/<system>/ for external I/O but never import
reconcile/ packages. Allowed imports from pkg/splunk/:

	common/, util/, client/
*/
package workflow
