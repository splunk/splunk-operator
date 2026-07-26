// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/splunk/splunk-operator/pkg/config"
)

const searchHeadRuntimeShutdownExecutable = "/sbin/splunk-shutdown"

// searchHeadPreStopScript delegates shutdown to the image's shared,
// idempotent runtime contract. Older images do not have that executable, so
// the compatibility path only withdraws the existing checkstate readiness and
// lets the image's TERM trap remain the single owner of "splunk stop".
const searchHeadPreStopScript = `
if [ -x "` + searchHeadRuntimeShutdownExecutable + `" ]; then
    echo "Search Head preStop is delegating to the runtime shutdown contract"
    exec "` + searchHeadRuntimeShutdownExecutable + `" --source=prestop
fi

container_artifact_dir="${CONTAINER_ARTIFACT_DIR:-/opt/container_artifact}"
state_file="${container_artifact_dir}/splunk-container.state"
temporary_state_file="${state_file}.stopping.$$"
printf '%s\n' stopping > "${temporary_state_file}"
mv -f "${temporary_state_file}" "${state_file}"
echo "Search Head shutdown intent recorded for a legacy image; waiting for Kubernetes TERM"
`

func searchHeadPodLifecycleEnabled(instanceType InstanceType) bool {
	return instanceType == SplunkSearchHead &&
		config.DefaultMutableFeatureGate.Enabled(config.SplunkPodLifecycle) &&
		config.DefaultMutableFeatureGate.Enabled(config.SearchHeadClusterLifecycle)
}

func applySearchHeadPodLifecycle(container *corev1.Container, instanceType InstanceType) {
	if container == nil || container.Name != "splunk" || !searchHeadPodLifecycleEnabled(instanceType) {
		return
	}

	lifecycle := container.Lifecycle.DeepCopy()
	if lifecycle == nil {
		lifecycle = &corev1.Lifecycle{}
	}
	lifecycle.PreStop = &corev1.LifecycleHandler{
		Exec: &corev1.ExecAction{
			Command: []string{"/bin/sh", "-ec", searchHeadPreStopScript},
		},
	}
	container.Lifecycle = lifecycle
}
