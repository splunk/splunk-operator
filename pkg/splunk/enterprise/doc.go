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
Package enterprise is LEGACY — it manages configuration for Splunk Enterprise
deployments but is being incrementally decomposed into purpose-built packages:

  - Per-CR reconcile logic       → reconcile/<cr>/
  - Multi-step workflows         → workflow/<domain>/
  - K8s object builders          → resources/
  - Admission webhooks           → validation/

New code should go into the target packages. This package shrinks over time and
will be deleted once all logic has been migrated.
*/
package enterprise
