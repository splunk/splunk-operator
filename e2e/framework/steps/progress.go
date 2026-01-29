package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	"github.com/splunk/splunk-operator/e2e/framework/topology"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"go.uber.org/zap"
)

func startTopologyProgressLogger(ctx context.Context, exec *Context, session *topology.Session, timeout time.Duration) func() {
	if exec == nil || exec.Logger == nil || exec.Kube == nil || exec.Config == nil {
		return func() {}
	}
	interval := exec.Config.ProgressInterval
	if interval <= 0 {
		return func() {}
	}

	progressCtx, cancel := context.WithCancel(ctx)
	start := time.Now()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-progressCtx.Done():
				return
			case <-ticker.C:
				logTopologyProgress(progressCtx, exec.Logger, exec.Kube, session, time.Since(start), timeout)
			}
		}
	}()

	return cancel
}

func logTopologyProgress(ctx context.Context, logger *zap.Logger, kube *k8s.Client, session *topology.Session, elapsed, timeout time.Duration) {
	if logger == nil || kube == nil || session == nil {
		return
	}
	fields := []zap.Field{
		zap.String("namespace", session.Namespace),
		zap.String("kind", session.Kind),
		zap.Duration("elapsed", elapsed),
	}
	if timeout > 0 {
		fields = append(fields, zap.Duration("timeout", timeout))
	}

	if session.StandaloneName != "" {
		fields = append(fields, zap.String("standalone_phase", standalonePhase(ctx, kube, session.Namespace, session.StandaloneName)))
	}
	if session.ClusterManagerName != "" {
		fields = append(fields, zap.String("cluster_manager_phase", clusterManagerPhase(ctx, kube, session)))
	}
	if len(session.IndexerClusterNames) > 0 {
		fields = append(fields, zap.String("indexer_phases", indexerPhases(ctx, kube, session.Namespace, session.IndexerClusterNames)))
	}
	if session.SearchHeadClusterName != "" {
		fields = append(fields, zap.String("search_head_phase", searchHeadPhase(ctx, kube, session.Namespace, session.SearchHeadClusterName)))
	}

	logger.Info("topology wait progress", fields...)
}

func standalonePhase(ctx context.Context, kube *k8s.Client, namespace, name string) string {
	instance := &enterpriseApi.Standalone{}
	if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
		return phaseError(err)
	}
	return string(instance.Status.Phase)
}

func clusterManagerPhase(ctx context.Context, kube *k8s.Client, session *topology.Session) string {
	if session.ClusterManagerKind == "master" {
		instance := &enterpriseApiV3.ClusterMaster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: session.ClusterManagerName, Namespace: session.Namespace}, instance); err != nil {
			return phaseError(err)
		}
		return string(instance.Status.Phase)
	}
	instance := &enterpriseApi.ClusterManager{}
	if err := kube.Client.Get(ctx, client.ObjectKey{Name: session.ClusterManagerName, Namespace: session.Namespace}, instance); err != nil {
		return phaseError(err)
	}
	return string(instance.Status.Phase)
}

func indexerPhases(ctx context.Context, kube *k8s.Client, namespace string, names []string) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		instance := &enterpriseApi.IndexerCluster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			parts = append(parts, fmt.Sprintf("%s=%s", name, phaseError(err)))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", name, instance.Status.Phase))
	}
	return strings.Join(parts, ",")
}

func searchHeadPhase(ctx context.Context, kube *k8s.Client, namespace, name string) string {
	instance := &enterpriseApi.SearchHeadCluster{}
	if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
		return phaseError(err)
	}
	return fmt.Sprintf("%s/%s", instance.Status.Phase, instance.Status.DeployerPhase)
}

func phaseError(err error) string {
	if apierrors.IsNotFound(err) {
		return "missing"
	}
	if err != nil {
		return "error"
	}
	return "unknown"
}
