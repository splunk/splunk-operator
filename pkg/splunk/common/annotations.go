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

package common

// ConfigMapRevAnnotationPrefix is the annotation key prefix stamped on pod template specs for
// each user-supplied ConfigMap volume. The suffix is the volume name (a valid DNS label, ≤63
// chars), which keeps the full key within Kubernetes' annotation key limits regardless of the
// ConfigMap name length.
//
// The annotation value is a short SHA256 content hash. It changes only when the mounted data
// changes, not on metadata-only updates to the ConfigMap, preventing spurious pod rolls.
const ConfigMapRevAnnotationPrefix = "revision.configmap.enterprise.splunk.com/"

// ConfigMapRestartOptOutAnnotation is the annotation key placed on a user-supplied ConfigMap to
// opt out of operator-triggered rolling restarts when that ConfigMap's data changes. Set its
// value to "false" to suppress restarts. Any other value (or absence of the annotation) keeps
// the default behavior: pods roll whenever the mounted content changes.
//
// Use this for ConfigMaps whose consumers (sidecar processes or apps) reload files dynamically,
// so Kubernetes can propagate the updated files on disk without a pod restart.
//
// The domain matches the operator's own annotation prefix (revision.configmap.enterprise.splunk.com/)
// to keep all operator-managed annotation keys under a single, consistent namespace.
const ConfigMapRestartOptOutAnnotation = "enterprise.splunk.com/configmap-restart"
