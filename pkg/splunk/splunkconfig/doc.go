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
Package splunkconfig composes the Splunk configuration entries (default.yml
ConfFileEntry values) for one SOK-managed role out of independent features.

This package owns composition only. Splunk domain knowledge — which stanzas a
role needs, how a queue provider spells its fields — lives in the ConfFeature
implementations under this directory, one per configuration concern. A feature is
constructed with its own already-resolved inputs and injected into a
RoleConfBuilder, so adding a feature requires no change here.

Reconcilers call ForRole, inject the features that apply to their CR, and deliver
the resulting ConfSet: NonSensitive through a ConfigMap, Secrets through a Secret.

Allowed imports from pkg/splunk/: common/, util/
*/
package splunkconfig
