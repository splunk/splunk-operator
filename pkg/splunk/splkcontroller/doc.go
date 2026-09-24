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
Package splkcontroller manipulates Kubernetes resources using its REST API.
This package has no dependencies outside of the standard go and kubernetes
libraries, and the splunk.common package.

This package will be renamed to k8sops/ to reflect its actual scope (full K8s
CRUD) and avoid confusion with the new reconcile/<cr>/ packages. See
pkg/splunk/k8sops/doc.go for the target documentation.
*/
package splkcontroller
