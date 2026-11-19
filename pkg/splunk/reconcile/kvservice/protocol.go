// Copyright (c) 2026 Splunk Inc. All rights reserved.
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

package kvservice

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const clientConfigurationReady = "ClientConfigurationReady"

// Client configuration is diagnostic: a mismatch must not suspend databases,
// authentication publication or serving for other namespaces.
func setClientProtocolCondition(cr *enterpriseApi.KVService, result tenantPostgresResult) {
	condition := metav1.Condition{Type: clientConfigurationReady, ObservedGeneration: cr.Generation,
		Status: metav1.ConditionUnknown, Reason: "DiscoveryPending", Message: "Namespace client protocols have not been evaluated"}
	if result.clientProtocolChecked {
		condition.Status, condition.Reason = metav1.ConditionTrue, "ListenerProtocolMatched"
		condition.Message = "All selected namespaces use the configured listener protocol"
		if len(result.protocolMismatches) > 0 {
			scheme := cr.Spec.Scheme
			condition.Status, condition.Reason = metav1.ConditionFalse, "ListenerProtocolMismatch"
			// Keep the condition and Event bounded even with many tenant namespaces.
			names := result.protocolMismatches
			if len(names) > 5 {
				names = names[:5]
			}
			condition.Message = fmt.Sprintf("Namespaces %q (%d total) use a different protocol from spec.scheme=%s. Set annotation %s to %s://%s in these namespaces and coordinate the change with Splunk clients.", names, len(result.protocolMismatches), scheme, splcommon.AnnotationKVServiceAddr, scheme, splutil.KVServiceBackendAddress(cr.Name, cr.Namespace))
		}
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)
}

// Emit diagnostics only after status persistence and only on transitions.
func recordClientProtocolEvent(ctx context.Context, publisher splcommon.EventPublisher, cr *enterpriseApi.KVService, previous []metav1.Condition) {
	current := meta.FindStatusCondition(cr.Status.Conditions, clientConfigurationReady)
	if current == nil {
		return
	}
	old := meta.FindStatusCondition(previous, clientConfigurationReady)
	if old != nil && old.Status == current.Status && old.Reason == current.Reason && old.Message == current.Message {
		return
	}
	if current.Status == metav1.ConditionFalse {
		logging.FromContext(ctx).WarnContext(ctx, "KVService client protocol mismatch", "name", cr.Name, "namespace", cr.Namespace, "message", current.Message)
		publisher.Warning(ctx, current.Reason, current.Message)
	} else if current.Status == metav1.ConditionTrue && old != nil && old.Status == metav1.ConditionFalse {
		publisher.Normal(ctx, "ListenerProtocolMatched", current.Message)
	}
}
