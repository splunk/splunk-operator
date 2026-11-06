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

package k8sops

import (
	"context"
	"fmt"
	"slices"

	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// KVServiceSettings holds the namespace-selected client settings written to 100-sok.
type KVServiceSettings struct {
	ConnectionString string
	Tenant           string
}

// SupportsKVService reports whether this Enterprise CR consumes KVService settings.
// IngestorCluster and backend resources do not consume these settings.
func SupportsKVService(cr splcommon.MetaObject) bool {
	switch cr.(type) {
	case *enterpriseApi.Standalone, *enterpriseApi.IndexerCluster,
		*enterpriseApi.SearchHeadCluster, *enterpriseApi.ClusterManager,
		*enterpriseApi.LicenseManager, *enterpriseApi.MonitoringConsole,
		*enterpriseApiV3.Standalone, *enterpriseApiV3.IndexerCluster,
		*enterpriseApiV3.SearchHeadCluster, *enterpriseApiV3.ClusterMaster,
		*enterpriseApiV3.LicenseMaster, *enterpriseApiV3.MonitoringConsole:
		return true
	default:
		return false
	}
}

// GetKVServiceSettings reads the consumer namespace's enable and tenant labels
// and address annotation. Supported short Service DNS addresses are used unchanged.
// It does not require a KVService CR, Service, or ready backend to exist.
func GetKVServiceSettings(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType) (KVServiceSettings, bool, error) {
	if !SupportsKVService(cr) || instanceType == SplunkDeployer {
		return KVServiceSettings{}, false, nil
	}
	watchNamespaces := config.GetWatchNamespaces()
	if len(watchNamespaces) > 0 && !slices.Contains(watchNamespaces, cr.GetNamespace()) {
		return KVServiceSettings{}, false, fmt.Errorf("namespace %q is not watched by the operator", cr.GetNamespace())
	}
	namespace := &corev1.Namespace{}
	if err := client.Get(ctx, types.NamespacedName{Name: cr.GetNamespace()}, namespace); err != nil {
		return KVServiceSettings{}, false, fmt.Errorf("get KVService configuration from namespace %q: %w", cr.GetNamespace(), err)
	}
	if namespace.Labels[splcommon.LabelKVService] != splcommon.LabelKVServiceEnabled {
		return KVServiceSettings{}, false, nil
	}
	address := namespace.Annotations[splcommon.AnnotationKVServiceAddr]
	tenant, err := splutil.GetTenantName(namespace)
	if err != nil {
		return KVServiceSettings{}, false, err
	}
	if err := splutil.ValidateKVServiceTenantName(tenant); err != nil {
		return KVServiceSettings{}, false, fmt.Errorf("validate tenant label in namespace %q: %w", namespace.Name, err)
	}
	if _, err := namespaceKVServiceAddress(ctx, namespace); err != nil {
		return KVServiceSettings{}, false, fmt.Errorf("resolve namespace KVService address: %w", err)
	}
	return KVServiceSettings{ConnectionString: address, Tenant: tenant}, true, nil
}

// namespaceKVServiceAddress supplies the same actionable, sanitized diagnostic at
// every namespace-metadata boundary. The annotation value itself is never logged.
func namespaceKVServiceAddress(ctx context.Context, namespace *corev1.Namespace) (*splutil.KVServiceAddress, error) {
	endpoint, err := splutil.ParseKVServiceAddress(namespace.Annotations[splcommon.AnnotationKVServiceAddr])
	if err != nil {
		err = fmt.Errorf("namespace %q: set annotation %s to a supported KVService address: %w", namespace.Name, splcommon.AnnotationKVServiceAddr, err)
		logging.FromContext(ctx).WarnContext(ctx, "invalid namespace KVService address", "namespace", namespace.Name, "annotation", splcommon.AnnotationKVServiceAddr, "error", err)
		return nil, err
	}
	return endpoint, nil
}
