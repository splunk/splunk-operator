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
Package reconcile contains per-CR orchestration sub-packages. Each sub-package
owns the thin reconcile loop for a single Custom Resource type: it reads the CR,
builds Kubernetes objects via resources/, applies them via k8sops/, delegates
multi-step workflows to workflow/<domain>/, and writes status.

Sub-packages must never import each other. Allowed imports from pkg/splunk/:

	common/, util/, resources/, k8sops/, client/, workflow/
*/
package reconcile
