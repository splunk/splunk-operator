/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package majorupgradeadapter

import "sigs.k8s.io/controller-runtime/pkg/client"

type upgradeEventEmitter interface {
	EmitNormalEvent(obj client.Object, reason, message string)
	EmitWarningEvent(obj client.Object, reason, message string)
}

type UpgradeNotifier struct {
	emitter upgradeEventEmitter
	cluster client.Object
}

func NewUpgradeNotifier(emitter upgradeEventEmitter, cluster client.Object) *UpgradeNotifier {
	return &UpgradeNotifier{emitter: emitter, cluster: cluster}
}

func (n *UpgradeNotifier) Inform(reason, message string) {
	n.emitter.EmitNormalEvent(n.cluster, reason, message)
}

func (n *UpgradeNotifier) Warn(reason, message string) {
	n.emitter.EmitWarningEvent(n.cluster, reason, message)
}
