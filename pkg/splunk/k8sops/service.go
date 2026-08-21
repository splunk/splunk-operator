// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package k8sops

import (
	"context"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyService creates or updates a Kubernetes Service
func ApplyService(ctx context.Context, client splcommon.ControllerClient, revised *corev1.Service) error {
	scopedLog := logging.FromContext(ctx).With("func", "ApplyService",
		"name", revised.GetObjectMeta().GetName(),
		"namespace", revised.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current corev1.Service

	err := client.Get(ctx, namespacedName, &current)
	if err != nil && k8serrors.IsNotFound(err) {
		return splutil.CreateResource(ctx, client, revised)
	} else if err != nil {
		return err
	}

	// check for changes in service template
	hasUpdates := MergeServiceSpecUpdates(ctx, &current.Spec, &revised.Spec, current.GetObjectMeta().GetName())
	*revised = current // caller expects that object passed represents latest state

	// only update if there are material differences, as determined by comparison function
	if hasUpdates {
		scopedLog.InfoContext(ctx, "updating existing Service")
		err = splutil.UpdateResource(ctx, client, revised)
		if err != nil {
			return err
		}
		err = client.Get(ctx, namespacedName, revised)
		if err != nil {
			return err
		}
	}

	// all is good!
	scopedLog.InfoContext(ctx, "no update to existing Service")
	return nil
}

// MergeServiceSpecUpdates merges the current and revised spec of the service object
func MergeServiceSpecUpdates(ctx context.Context, current *corev1.ServiceSpec, revised *corev1.ServiceSpec, name string) bool {
	scopedLog := logging.FromContext(ctx).With("func", "MergeServiceSpecUpdates", "name", name)
	result := false

	// check service Type. An empty revised.Type means the controller did not
	// explicitly set one; Kubernetes defaults it to ClusterIP server-side.
	// Treat the empty value as ClusterIP so we (1) avoid an endless
	// reconcile->update->watch loop against the API-server-defaulted ClusterIP,
	// while (2) still driving a previously customized Service (e.g. LoadBalancer
	// or NodePort set via spec.serviceTemplate) back to the default ClusterIP
	// when the override is removed from the CR.
	currentType := current.Type
	if currentType == "" {
		currentType = corev1.ServiceTypeClusterIP
	}
	revisedType := revised.Type
	if revisedType == "" {
		revisedType = corev1.ServiceTypeClusterIP
	}
	if currentType != revisedType {
		scopedLog.InfoContext(ctx, "service Type differs",
			"current", current.Type,
			"revised", revisedType)
		current.Type = revisedType
		result = true
	}

	if current.ExternalName != revised.ExternalName {
		scopedLog.InfoContext(ctx, "external Name differs",
			"current", current.ExternalName,
			"revised", revised.ExternalName)
		current.ExternalName = revised.ExternalName
		result = true
	}

	if current.ExternalTrafficPolicy != revised.ExternalTrafficPolicy {
		scopedLog.InfoContext(ctx, "external Traffic Policy differs",
			"current", current.ExternalTrafficPolicy,
			"revised", revised.ExternalTrafficPolicy)
		current.ExternalTrafficPolicy = revised.ExternalTrafficPolicy
		result = true
	}

	if splcommon.CompareSortedStrings(current.ExternalIPs, revised.ExternalIPs) {
		scopedLog.InfoContext(ctx, "external IPs differs",
			"current", current.ExternalIPs,
			"revised", revised.ExternalIPs)
		current.ExternalIPs = revised.ExternalIPs
		result = true
	}

	// check for changes in Ports
	if splcommon.CompareServicePorts(current.Ports, revised.Ports) {
		scopedLog.InfoContext(ctx, "service Ports differs",
			"current", current.Ports,
			"revised", revised.Ports)
		current.Ports = revised.Ports
		result = true
	}

	return result
}
