package topology

import (
	"context"
	"fmt"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeployStandalone creates a standalone CR.
func DeployStandalone(ctx context.Context, kube *k8s.Client, namespace, name, splunkImage, serviceAccount, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) (*enterpriseApi.Standalone, error) {
	if serviceAccount != "" {
		serviceAccountObj := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceAccount,
				Namespace: namespace,
			},
		}
		if err := kube.Client.Create(ctx, serviceAccountObj); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}
	}
	standalone := newStandalone(name, namespace, splunkImage, serviceAccount, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
	if err := kube.Client.Create(ctx, standalone); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
	}
	return standalone, nil
}

// DeploySingleSiteCluster creates cluster manager, indexers, and SHC.
func DeploySingleSiteCluster(ctx context.Context, kube *k8s.Client, namespace, baseName, splunkImage string, indexerReplicas, shcReplicas int32, withSHC bool, clusterManagerKind, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) error {
	useClusterMaster := strings.EqualFold(clusterManagerKind, "master")
	if useClusterMaster {
		cm := newClusterMaster(baseName, namespace, splunkImage, "", licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, cm); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
		}
	} else {
		cm := newClusterManager(baseName, namespace, splunkImage, "", licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, cm); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	clusterManagerRef := ""
	clusterMasterRef := ""
	if useClusterMaster {
		clusterMasterRef = baseName
	} else {
		clusterManagerRef = baseName
	}

	idxc := newIndexerCluster(baseName+"-idxc", namespace, clusterManagerRef, clusterMasterRef, splunkImage, indexerReplicas, "", licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
	if err := kube.Client.Create(ctx, idxc); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	if withSHC {
		shc := newSearchHeadCluster(baseName+"-shc", namespace, clusterManagerRef, clusterMasterRef, splunkImage, shcReplicas, "", licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, shc); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	return nil
}

// DeployMultisiteCluster creates multisite cluster manager and indexer sites (no SHC).
func DeployMultisiteCluster(ctx context.Context, kube *k8s.Client, namespace, baseName, splunkImage string, indexerReplicas int32, siteCount int, clusterManagerKind, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) ([]string, error) {
	if siteCount < 1 {
		return nil, fmt.Errorf("siteCount must be >= 1")
	}
	allSites := make([]string, 0, siteCount)
	for i := 1; i <= siteCount; i++ {
		allSites = append(allSites, fmt.Sprintf("site%d", i))
	}

	clusterDefaults := fmt.Sprintf(`splunk:
  multisite_master: localhost
  all_sites: %s
  site: site1
  multisite_replication_factor_origin: 1
  multisite_replication_factor_total: 2
  multisite_search_factor_origin: 1
  multisite_search_factor_total: 2
  idxc:
    search_factor: 2
    replication_factor: 2
`, strings.Join(allSites, ","))

	useClusterMaster := strings.EqualFold(clusterManagerKind, "master")
	if useClusterMaster {
		cm := newClusterMaster(baseName, namespace, splunkImage, clusterDefaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, cm); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}
	} else {
		cm := newClusterManager(baseName, namespace, splunkImage, clusterDefaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, cm); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}
	}

	clusterManagerRef := ""
	clusterMasterRef := ""
	clusterRole := "cluster-manager"
	if useClusterMaster {
		clusterMasterRef = baseName
		clusterRole = "cluster-master"
	} else {
		clusterManagerRef = baseName
	}

	indexerNames := make([]string, 0, siteCount)
	for i := 1; i <= siteCount; i++ {
		siteName := fmt.Sprintf("site%d", i)
		siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: %s
`, baseName, clusterRole, siteName)
		idxcName := fmt.Sprintf("%s-%s", baseName, siteName)
		idxc := newIndexerCluster(idxcName, namespace, clusterManagerRef, clusterMasterRef, splunkImage, indexerReplicas, siteDefaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, idxc); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}
		indexerNames = append(indexerNames, idxcName)
	}

	return indexerNames, nil
}

// DeployMultisiteClusterWithSearchHead creates multisite cluster manager, indexer sites, and SHC.
func DeployMultisiteClusterWithSearchHead(ctx context.Context, kube *k8s.Client, namespace, baseName, splunkImage string, indexerReplicas, shcReplicas int32, siteCount int, clusterManagerKind, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) ([]string, error) {
	if siteCount < 1 {
		return nil, fmt.Errorf("siteCount must be >= 1")
	}
	allSites := make([]string, 0, siteCount)
	for i := 1; i <= siteCount; i++ {
		allSites = append(allSites, fmt.Sprintf("site%d", i))
	}

	clusterDefaults := fmt.Sprintf(`splunk:
  multisite_master: localhost
  all_sites: %s
  site: site1
  multisite_replication_factor_origin: 1
  multisite_replication_factor_total: 2
  multisite_search_factor_origin: 1
  multisite_search_factor_total: 2
  idxc:
    search_factor: 2
    replication_factor: 2
`, strings.Join(allSites, ","))

	useClusterMaster := strings.EqualFold(clusterManagerKind, "master")
	if useClusterMaster {
		cm := newClusterMaster(baseName, namespace, splunkImage, clusterDefaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, cm); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}
	} else {
		cm := newClusterManager(baseName, namespace, splunkImage, clusterDefaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, cm); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}
	}

	clusterManagerRef := ""
	clusterMasterRef := ""
	clusterRole := "cluster-manager"
	if useClusterMaster {
		clusterMasterRef = baseName
		clusterRole = "cluster-master"
	} else {
		clusterManagerRef = baseName
	}

	indexerNames := make([]string, 0, siteCount)
	for i := 1; i <= siteCount; i++ {
		siteName := fmt.Sprintf("site%d", i)
		siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: %s
`, baseName, clusterRole, siteName)
		idxcName := fmt.Sprintf("%s-%s", baseName, siteName)
		idxc := newIndexerCluster(idxcName, namespace, clusterManagerRef, clusterMasterRef, splunkImage, indexerReplicas, siteDefaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
		if err := kube.Client.Create(ctx, idxc); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}
		indexerNames = append(indexerNames, idxcName)
	}

	shcDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: site0
`, baseName, clusterRole)
	shc := newSearchHeadCluster(baseName+"-shc", namespace, clusterManagerRef, clusterMasterRef, splunkImage, shcReplicas, shcDefaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef)
	if err := kube.Client.Create(ctx, shc); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
	}

	return indexerNames, nil
}
