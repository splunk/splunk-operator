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
Package resources builds Kubernetes objects (StatefulSets, Services, ConfigMaps,
PVCs, volumes, probes, labels, env vars). It takes specs as input and returns
constructed objects — no client.Create/Update calls, no external API calls, no I/O.

Migrated from enterprise/configuration.go and related K8s object builder
functions. Allowed imports from pkg/splunk/:

	common/, util/
*/
package resources
