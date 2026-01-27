package topology

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/k8s"
)

// Options defines deployment parameters for a topology.
type Options struct {
	Kind                 string
	Namespace            string
	BaseName             string
	SplunkImage          string
	ServiceAccount       string
	LicenseManagerRef    string
	LicenseMasterRef     string
	MonitoringConsoleRef string
	ClusterManagerKind   string
	IndexerReplicas      int32
	SHCReplicas          int32
	WithSHC              bool
	SiteCount            int
}

// Session captures deployed topology details.
type Session struct {
	Kind                  string
	Namespace             string
	BaseName              string
	StandaloneName        string
	ClusterManagerName    string
	ClusterManagerKind    string
	IndexerClusterNames   []string
	SearchHeadClusterName string
	SearchPod             string
	SiteCount             int
}

// Deploy creates topology resources and returns a session.
func Deploy(ctx context.Context, kube *k8s.Client, opts Options) (*Session, error) {
	kind := strings.ToLower(strings.TrimSpace(opts.Kind))
	if kind == "" {
		return nil, fmt.Errorf("topology kind is required")
	}
	clusterManagerKind := normalizeClusterManagerKind(opts.ClusterManagerKind, opts.BaseName)
	session := &Session{
		Kind:               kind,
		Namespace:          opts.Namespace,
		BaseName:           opts.BaseName,
		SiteCount:          opts.SiteCount,
		ClusterManagerKind: clusterManagerKind,
	}

	switch kind {
	case "s1":
		standaloneName := opts.BaseName
		if _, err := DeployStandalone(ctx, kube, opts.Namespace, standaloneName, opts.SplunkImage, opts.ServiceAccount, opts.LicenseManagerRef, opts.LicenseMasterRef, opts.MonitoringConsoleRef); err != nil {
			return nil, err
		}
		session.StandaloneName = standaloneName
		session.SearchPod = fmt.Sprintf("splunk-%s-standalone-0", standaloneName)
	case "c3":
		if opts.IndexerReplicas < 1 {
			opts.IndexerReplicas = 3
		}
		if opts.SHCReplicas < 1 {
			opts.SHCReplicas = 3
		}
		if err := DeploySingleSiteCluster(ctx, kube, opts.Namespace, opts.BaseName, opts.SplunkImage, opts.IndexerReplicas, opts.SHCReplicas, opts.WithSHC, clusterManagerKind, opts.LicenseManagerRef, opts.LicenseMasterRef, opts.MonitoringConsoleRef); err != nil {
			return nil, err
		}
		session.ClusterManagerName = opts.BaseName
		session.IndexerClusterNames = []string{opts.BaseName + "-idxc"}
		if opts.WithSHC {
			session.SearchHeadClusterName = opts.BaseName + "-shc"
			session.SearchPod = fmt.Sprintf("splunk-%s-shc-search-head-0", opts.BaseName)
		}
	case "m1":
		if opts.SiteCount < 1 {
			opts.SiteCount = 3
		}
		if opts.IndexerReplicas < 1 {
			opts.IndexerReplicas = 1
		}
		session.SiteCount = opts.SiteCount
		indexerNames, err := DeployMultisiteCluster(ctx, kube, opts.Namespace, opts.BaseName, opts.SplunkImage, opts.IndexerReplicas, opts.SiteCount, clusterManagerKind, opts.LicenseManagerRef, opts.LicenseMasterRef, opts.MonitoringConsoleRef)
		if err != nil {
			return nil, err
		}
		session.ClusterManagerName = opts.BaseName
		session.IndexerClusterNames = indexerNames
		searchRole := "cluster-manager"
		if clusterManagerKind == "master" {
			searchRole = "cluster-master"
		}
		session.SearchPod = fmt.Sprintf("splunk-%s-%s-0", opts.BaseName, searchRole)
	case "m4":
		if opts.SiteCount < 1 {
			opts.SiteCount = 3
		}
		if opts.IndexerReplicas < 1 {
			opts.IndexerReplicas = 1
		}
		if opts.SHCReplicas < 1 {
			opts.SHCReplicas = 3
		}
		session.SiteCount = opts.SiteCount
		indexerNames, err := DeployMultisiteClusterWithSearchHead(ctx, kube, opts.Namespace, opts.BaseName, opts.SplunkImage, opts.IndexerReplicas, opts.SHCReplicas, opts.SiteCount, clusterManagerKind, opts.LicenseManagerRef, opts.LicenseMasterRef, opts.MonitoringConsoleRef)
		if err != nil {
			return nil, err
		}
		session.ClusterManagerName = opts.BaseName
		session.IndexerClusterNames = indexerNames
		session.SearchHeadClusterName = opts.BaseName + "-shc"
		session.SearchPod = fmt.Sprintf("splunk-%s-shc-search-head-0", opts.BaseName)
	default:
		return nil, fmt.Errorf("unsupported topology kind: %s", kind)
	}

	return session, nil
}

// WaitReady waits for all resources in a topology session to become ready.
func WaitReady(ctx context.Context, kube *k8s.Client, session *Session, timeout time.Duration) error {
	switch session.Kind {
	case "s1":
		return WaitStandaloneReady(ctx, kube, session.Namespace, session.StandaloneName, timeout)
	case "c3":
		if err := waitClusterManagerReady(ctx, kube, session, timeout); err != nil {
			return err
		}
		if len(session.IndexerClusterNames) > 0 {
			if err := WaitIndexerClusterReady(ctx, kube, session.Namespace, session.IndexerClusterNames[0], timeout); err != nil {
				return err
			}
		}
		if session.SearchHeadClusterName != "" {
			if err := WaitSearchHeadClusterReady(ctx, kube, session.Namespace, session.SearchHeadClusterName, timeout); err != nil {
				return err
			}
		}
		return nil
	case "m1":
		if err := waitClusterManagerReady(ctx, kube, session, timeout); err != nil {
			return err
		}
		for _, name := range session.IndexerClusterNames {
			if err := WaitIndexerClusterReady(ctx, kube, session.Namespace, name, timeout); err != nil {
				return err
			}
		}
		return nil
	case "m4":
		if err := waitClusterManagerReady(ctx, kube, session, timeout); err != nil {
			return err
		}
		for _, name := range session.IndexerClusterNames {
			if err := WaitIndexerClusterReady(ctx, kube, session.Namespace, name, timeout); err != nil {
				return err
			}
		}
		if session.SearchHeadClusterName != "" {
			if err := WaitSearchHeadClusterReady(ctx, kube, session.Namespace, session.SearchHeadClusterName, timeout); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported topology kind: %s", session.Kind)
	}
}

// WaitStable checks topology resources stay ready for a duration.
func WaitStable(ctx context.Context, kube *k8s.Client, session *Session, duration, interval time.Duration) error {
	switch session.Kind {
	case "s1":
		return WaitStandaloneStable(ctx, kube, session.Namespace, session.StandaloneName, duration, interval)
	case "c3":
		if err := waitClusterManagerStable(ctx, kube, session, duration, interval); err != nil {
			return err
		}
		if len(session.IndexerClusterNames) > 0 {
			if err := WaitIndexerClusterStable(ctx, kube, session.Namespace, session.IndexerClusterNames[0], duration, interval); err != nil {
				return err
			}
		}
		if session.SearchHeadClusterName != "" {
			if err := WaitSearchHeadClusterStable(ctx, kube, session.Namespace, session.SearchHeadClusterName, duration, interval); err != nil {
				return err
			}
		}
		return nil
	case "m1":
		if err := waitClusterManagerStable(ctx, kube, session, duration, interval); err != nil {
			return err
		}
		for _, name := range session.IndexerClusterNames {
			if err := WaitIndexerClusterStable(ctx, kube, session.Namespace, name, duration, interval); err != nil {
				return err
			}
		}
		return nil
	case "m4":
		if err := waitClusterManagerStable(ctx, kube, session, duration, interval); err != nil {
			return err
		}
		for _, name := range session.IndexerClusterNames {
			if err := WaitIndexerClusterStable(ctx, kube, session.Namespace, name, duration, interval); err != nil {
				return err
			}
		}
		if session.SearchHeadClusterName != "" {
			if err := WaitSearchHeadClusterStable(ctx, kube, session.Namespace, session.SearchHeadClusterName, duration, interval); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported topology kind: %s", session.Kind)
	}
}

func normalizeClusterManagerKind(value, baseName string) string {
	kind := strings.ToLower(strings.TrimSpace(value))
	switch kind {
	case "master", "cluster-master", "clustermaster":
		return "master"
	case "manager", "cluster-manager", "clustermanager":
		return "manager"
	}
	if kind == "" {
		if strings.Contains(strings.ToLower(baseName), "master") {
			return "master"
		}
		return "manager"
	}
	return "manager"
}

func waitClusterManagerReady(ctx context.Context, kube *k8s.Client, session *Session, timeout time.Duration) error {
	if session.ClusterManagerKind == "master" {
		return WaitClusterMasterReady(ctx, kube, session.Namespace, session.ClusterManagerName, timeout)
	}
	return WaitClusterManagerReady(ctx, kube, session.Namespace, session.ClusterManagerName, timeout)
}

func waitClusterManagerStable(ctx context.Context, kube *k8s.Client, session *Session, duration, interval time.Duration) error {
	if session.ClusterManagerKind == "master" {
		return WaitClusterMasterStable(ctx, kube, session.Namespace, session.ClusterManagerName, duration, interval)
	}
	return WaitClusterManagerStable(ctx, kube, session.Namespace, session.ClusterManagerName, duration, interval)
}
