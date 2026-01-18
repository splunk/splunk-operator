package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/topology"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RegisterTopologyHandlers registers topology-related steps.
func RegisterTopologyHandlers(reg *Registry) {
	reg.Register("topology.deploy", handleTopologyDeploy)
	reg.Register("topology.wait_ready", handleTopologyWaitReady)
	reg.Register("topology.wait_stable", handleTopologyWaitStable)
}

func handleTopologyDeploy(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	kind := getString(step.With, "kind", exec.Spec.Topology.Kind)
	if kind == "" {
		return nil, fmt.Errorf("topology kind is required")
	}
	kind = strings.ToLower(kind)

	if exec.Vars["topology_ready"] == "true" {
		if existing := strings.ToLower(exec.Vars["topology_kind"]); existing != "" && existing != kind {
			return nil, fmt.Errorf("topology already initialized with kind %s", existing)
		}
		metadata := topologyMetadataFromVars(exec.Vars)
		metadata["shared"] = "true"
		return metadata, nil
	}

	namespace := expandVars(getStringFallback(step.With, exec.Spec.Topology.Params, "namespace", exec.Vars["namespace"]), exec.Vars)
	if namespace == "" {
		namespace = fmt.Sprintf("%s-%s", exec.Config.NamespacePrefix, topology.RandomDNSName(5))
	}

	if err := exec.Kube.EnsureNamespace(ctx, namespace); err != nil {
		return nil, err
	}

	baseName := expandVars(getStringFallback(step.With, exec.Spec.Topology.Params, "name", namespace), exec.Vars)
	serviceAccount := expandVars(getStringFallback(step.With, exec.Spec.Topology.Params, "service_account", ""), exec.Vars)
	licenseManagerRef := expandVars(getStringFallback(step.With, exec.Spec.Topology.Params, "license_manager_ref", ""), exec.Vars)
	licenseMasterRef := expandVars(getStringFallback(step.With, exec.Spec.Topology.Params, "license_master_ref", ""), exec.Vars)
	monitoringConsoleRef := expandVars(getStringFallback(step.With, exec.Spec.Topology.Params, "monitoring_console_ref", ""), exec.Vars)
	clusterManagerKind := expandVars(getStringFallback(step.With, exec.Spec.Topology.Params, "cluster_manager_kind", ""), exec.Vars)
	if serviceAccount != "" {
		serviceAccountObj := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceAccount,
				Namespace: namespace,
			},
		}
		if err := exec.Kube.Client.Create(ctx, serviceAccountObj); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
	}

	indexerReplicas := int32(getIntFallback(step.With, exec.Spec.Topology.Params, "indexer_replicas", 3))
	shcReplicas := int32(getIntFallback(step.With, exec.Spec.Topology.Params, "shc_replicas", 3))
	siteCount := getIntFallback(step.With, exec.Spec.Topology.Params, "site_count", 3)
	if siteCount == 0 {
		siteCount = getIntFallback(step.With, exec.Spec.Topology.Params, "sites", 3)
	}
	withSHC := getBoolFallback(step.With, exec.Spec.Topology.Params, "with_shc", true)

	session, err := topology.Deploy(ctx, exec.Kube, topology.Options{
		Kind:                 kind,
		Namespace:            namespace,
		BaseName:             baseName,
		SplunkImage:          exec.Config.SplunkImage,
		ServiceAccount:       serviceAccount,
		LicenseManagerRef:    licenseManagerRef,
		LicenseMasterRef:     licenseMasterRef,
		MonitoringConsoleRef: monitoringConsoleRef,
		ClusterManagerKind:   clusterManagerKind,
		IndexerReplicas:      indexerReplicas,
		SHCReplicas:          shcReplicas,
		WithSHC:              withSHC,
		SiteCount:            siteCount,
	})
	if err != nil {
		return nil, err
	}

	return ApplyTopologySession(exec, session), nil
}

func handleTopologyWaitReady(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec.Vars["topology_waited"] == "true" {
		return map[string]string{"shared": "true"}, nil
	}

	kind := getString(step.With, "kind", exec.Vars["topology_kind"])
	namespace := exec.Vars["namespace"]
	if kind == "" || namespace == "" {
		return nil, fmt.Errorf("topology kind and namespace are required")
	}

	timeout := exec.Config.DefaultTimeout
	if override := getString(step.With, "timeout", ""); override != "" {
		if parsed, err := time.ParseDuration(override); err == nil {
			timeout = parsed
		}
	}

	session := &topology.Session{
		Kind:                  strings.ToLower(kind),
		Namespace:             namespace,
		BaseName:              exec.Vars["base_name"],
		StandaloneName:        exec.Vars["standalone_name"],
		ClusterManagerName:    exec.Vars["cluster_manager_name"],
		ClusterManagerKind:    exec.Vars["cluster_manager_kind"],
		SearchHeadClusterName: exec.Vars["search_head_cluster_name"],
		SearchPod:             exec.Vars["search_pod"],
	}
	if idxc := exec.Vars["indexer_cluster_name"]; idxc != "" {
		session.IndexerClusterNames = []string{idxc}
	}
	if idxcList := exec.Vars["indexer_cluster_names"]; idxcList != "" {
		session.IndexerClusterNames = strings.Split(idxcList, ",")
	}

	if err := topology.WaitReady(ctx, exec.Kube, session, timeout); err != nil {
		return nil, err
	}
	exec.Vars["topology_waited"] = "true"
	return nil, nil
}

func handleTopologyWaitStable(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec.Vars["topology_stable"] == "true" {
		return map[string]string{"shared": "true"}, nil
	}

	kind := getString(step.With, "kind", exec.Vars["topology_kind"])
	namespace := exec.Vars["namespace"]
	if kind == "" || namespace == "" {
		return nil, fmt.Errorf("topology kind and namespace are required")
	}

	duration := time.Duration(0)
	if raw := getString(step.With, "duration", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			duration = parsed
		}
	}
	interval := time.Duration(0)
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}

	session := &topology.Session{
		Kind:                  strings.ToLower(kind),
		Namespace:             namespace,
		BaseName:              exec.Vars["base_name"],
		StandaloneName:        exec.Vars["standalone_name"],
		ClusterManagerName:    exec.Vars["cluster_manager_name"],
		ClusterManagerKind:    exec.Vars["cluster_manager_kind"],
		SearchHeadClusterName: exec.Vars["search_head_cluster_name"],
		SearchPod:             exec.Vars["search_pod"],
	}
	if idxc := exec.Vars["indexer_cluster_name"]; idxc != "" {
		session.IndexerClusterNames = []string{idxc}
	}
	if idxcList := exec.Vars["indexer_cluster_names"]; idxcList != "" {
		session.IndexerClusterNames = strings.Split(idxcList, ",")
	}

	if err := topology.WaitStable(ctx, exec.Kube, session, duration, interval); err != nil {
		return nil, err
	}
	exec.Vars["topology_stable"] = "true"
	return nil, nil
}

func topologyMetadataFromVars(vars map[string]string) map[string]string {
	metadata := map[string]string{
		"namespace":  vars["namespace"],
		"base_name":  vars["base_name"],
		"topology":   vars["topology_kind"],
		"search_pod": vars["search_pod"],
	}
	if value := vars["standalone_name"]; value != "" {
		metadata["standalone_name"] = value
	}
	if value := vars["cluster_manager_name"]; value != "" {
		metadata["cluster_manager_name"] = value
	}
	if value := vars["indexer_cluster_name"]; value != "" {
		metadata["indexer_cluster_name"] = value
	}
	if value := vars["indexer_cluster_names"]; value != "" {
		metadata["indexer_cluster_names"] = value
	}
	if value := vars["search_head_cluster_name"]; value != "" {
		metadata["search_head_cluster_name"] = value
	}
	if value := vars["cluster_manager_kind"]; value != "" {
		metadata["cluster_manager_kind"] = value
	}
	if value := vars["site_count"]; value != "" {
		metadata["site_count"] = value
	}
	return metadata
}
