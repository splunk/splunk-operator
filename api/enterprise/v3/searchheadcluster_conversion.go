/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package v3

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	hubApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

var _ conversion.Convertible = &SearchHeadCluster{}

// ConvertTo converts this v3 SearchHeadCluster (the receiver is the source) to
// the hub. Forward conversion always succeeds: every v3 field has a hub
// counterpart, and hub-only fields are left unset so a classic resource does not
// acquire Noah configuration.
func (c *SearchHeadCluster) ConvertTo(dstHub conversion.Hub) error {
	dst, ok := dstHub.(*hubApi.SearchHeadCluster)
	if !ok {
		return fmt.Errorf("unsupported conversion hub for SearchHeadCluster: %T", dstHub)
	}

	dst.ObjectMeta = c.ObjectMeta
	dst.Spec.CommonSplunkSpec = c.Spec.CommonSplunkSpec
	dst.Spec.Replicas = c.Spec.Replicas
	dst.Spec.AppFrameworkConfig = c.Spec.AppFrameworkConfig
	dst.Spec.DetentionTimeoutSeconds = c.Spec.DetentionTimeoutSeconds

	dst.Status = hubApi.SearchHeadClusterStatus{
		Phase:                          c.Status.Phase,
		DeployerPhase:                  c.Status.DeployerPhase,
		Replicas:                       c.Status.Replicas,
		ReadyReplicas:                  c.Status.ReadyReplicas,
		Selector:                       c.Status.Selector,
		Captain:                        c.Status.Captain,
		CaptainReady:                   c.Status.CaptainReady,
		Initialized:                    c.Status.Initialized,
		MinPeersJoined:                 c.Status.MinPeersJoined,
		MaintenanceMode:                c.Status.MaintenanceMode,
		ShcSecretChanged:               c.Status.ShcSecretChanged,
		AdminSecretChanged:             c.Status.AdminSecretChanged,
		AdminPasswordChangedSecrets:    c.Status.AdminPasswordChangedSecrets,
		NamespaceSecretResourceVersion: c.Status.NamespaceSecretResourceVersion,
		Members:                        convertMembersTo(c.Status.Members),
		AppContext:                     c.Status.AppContext,
		TelAppInstalled:                c.Status.TelAppInstalled,
		DetentionStartTimestamp:        c.Status.DetentionStartTimestamp,
		DetainedMemberName:             c.Status.DetainedMemberName,
		DetainedPodRevision:            c.Status.DetainedPodRevision,
	}

	return nil
}

// ConvertFrom converts the hub into this v3 SearchHeadCluster (the receiver is
// the destination). It refuses when the hub carries state v3 cannot represent,
// because succeeding would hand the caller an object silently missing that state
// — and a write-back would then erase it. Classic resources convert cleanly and
// keep working at v3.
func (c *SearchHeadCluster) ConvertFrom(srcHub conversion.Hub) error {
	src, ok := srcHub.(*hubApi.SearchHeadCluster)
	if !ok {
		return fmt.Errorf("unsupported conversion hub for SearchHeadCluster: %T", srcHub)
	}

	if err := refuseIfUnrepresentable("SearchHeadCluster", src.Namespace, src.Name,
		unrepresentableSearchHeadClusterFields(&src.Spec)); err != nil {
		return err
	}

	c.ObjectMeta = src.ObjectMeta
	c.Spec.CommonSplunkSpec = src.Spec.CommonSplunkSpec
	c.Spec.Replicas = src.Spec.Replicas
	c.Spec.AppFrameworkConfig = src.Spec.AppFrameworkConfig
	c.Spec.DetentionTimeoutSeconds = src.Spec.DetentionTimeoutSeconds

	c.Status = SearchHeadClusterStatus{
		Phase:                          src.Status.Phase,
		DeployerPhase:                  src.Status.DeployerPhase,
		Replicas:                       src.Status.Replicas,
		ReadyReplicas:                  src.Status.ReadyReplicas,
		Selector:                       src.Status.Selector,
		Captain:                        src.Status.Captain,
		CaptainReady:                   src.Status.CaptainReady,
		Initialized:                    src.Status.Initialized,
		MinPeersJoined:                 src.Status.MinPeersJoined,
		MaintenanceMode:                src.Status.MaintenanceMode,
		ShcSecretChanged:               src.Status.ShcSecretChanged,
		AdminSecretChanged:             src.Status.AdminSecretChanged,
		AdminPasswordChangedSecrets:    src.Status.AdminPasswordChangedSecrets,
		NamespaceSecretResourceVersion: src.Status.NamespaceSecretResourceVersion,
		Members:                        convertMembersFrom(src.Status.Members),
		AppContext:                     src.Status.AppContext,
		TelAppInstalled:                src.Status.TelAppInstalled,
		DetentionStartTimestamp:        src.Status.DetentionStartTimestamp,
		DetainedMemberName:             src.Status.DetainedMemberName,
		DetainedPodRevision:            src.Status.DetainedPodRevision,
	}

	return nil
}

func convertMembersTo(members []SearchHeadClusterMemberStatus) []hubApi.SearchHeadClusterMemberStatus {
	if members == nil {
		return nil
	}
	converted := make([]hubApi.SearchHeadClusterMemberStatus, 0, len(members))
	for _, member := range members {
		converted = append(converted, hubApi.SearchHeadClusterMemberStatus{
			Name:                        member.Name,
			Status:                      member.Status,
			Adhoc:                       member.Adhoc,
			Registered:                  member.Registered,
			ActiveHistoricalSearchCount: member.ActiveHistoricalSearchCount,
			ActiveRealtimeSearchCount:   member.ActiveRealtimeSearchCount,
			PodRevision:                 member.PodRevision,
		})
	}
	return converted
}

func convertMembersFrom(members []hubApi.SearchHeadClusterMemberStatus) []SearchHeadClusterMemberStatus {
	if members == nil {
		return nil
	}
	converted := make([]SearchHeadClusterMemberStatus, 0, len(members))
	for _, member := range members {
		converted = append(converted, SearchHeadClusterMemberStatus{
			Name:                        member.Name,
			Status:                      member.Status,
			Adhoc:                       member.Adhoc,
			Registered:                  member.Registered,
			ActiveHistoricalSearchCount: member.ActiveHistoricalSearchCount,
			ActiveRealtimeSearchCount:   member.ActiveRealtimeSearchCount,
			PodRevision:                 member.PodRevision,
		})
	}
	return converted
}
