package enterprise

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func UpgradePathValidation(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec enterpriseApi.CommonSplunkSpec, mgr *indexerClusterPodManager) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isClusterManagerReadyForUpgrade").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)
	kind :=  cr.GroupVersionKind().Kind
	scopedLog.Info("kind is set to ", "kind", kind)
	goto Standalone

Standalone:
	if cr.GroupVersionKind().Kind == "Standalone" {
		return true, nil
	} else {
		goto LicenseManager
	}
LicenseManager:
	if cr.GroupVersionKind().Kind == "LicenseManager" {
		return true, nil
	} else {
		licenseManagerRef := spec.LicenseManagerRef
		if licenseManagerRef.Name == "" {
			goto ClusterManager
		}

		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: licenseManagerRef.Name}
		licenseManager := &enterpriseApi.LicenseManager{}
		// get the license manager referred in CR
		err := c.Get(ctx, namespacedName, licenseManager)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				goto ClusterManager
			}
			return false, err
		}

		lmImage, err := getCurrentImage(ctx, c, licenseManager, SplunkLicenseManager)
		if err != nil {
			eventPublisher.Warning(ctx, "isClusterManagerReadyForUpgrade", fmt.Sprintf("Could not get the License Manager Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get licenseManager current image")
			return false, err
		}
		if licenseManager.Status.Phase != enterpriseApi.PhaseReady || lmImage != spec.Image {
			return false, err
		}
		goto ClusterManager
	}
ClusterManager:
	if cr.GroupVersionKind().Kind == "ClusterManager" {

		licenseManagerRef := spec.LicenseManagerRef
		if licenseManagerRef.Name == "" {
			return true, nil
		}
		namespacedName := types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      GetSplunkStatefulsetName(SplunkClusterManager, cr.GetName()),
		}

		// check if the stateful set is created at this instance
		statefulSet := &appsv1.StatefulSet{}
		err := c.Get(ctx, namespacedName, statefulSet)
		if err != nil && k8serrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	} else {
		// check if a LicenseManager is attached to the instance
		clusterManagerRef := spec.ClusterManagerRef
		if clusterManagerRef.Name == "" {
			goto MonitoringConsole
		}

		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: clusterManagerRef.Name}
		clusterManager := &enterpriseApi.ClusterManager{}

		// get the cluster manager referred in monitoring console
		err := c.Get(ctx, namespacedName, clusterManager)
		if err != nil {
			eventPublisher.Warning(ctx, "isMonitoringConsoleReadyForUpgrade", fmt.Sprintf("Could not find the Cluster Manager. Reason %v", err))
			scopedLog.Error(err, "Unable to get clusterManager")
			goto MonitoringConsole
		}

		cmImage, err := getCurrentImage(ctx, c, cr, SplunkClusterManager)
		if err != nil {
			eventPublisher.Warning(ctx, "isMonitoringConsoleReadyForUpgrade", fmt.Sprintf("Could not get the Cluster Manager Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get clusterManager current image")
			return false, err
		}

		// check if an image upgrade is happening and whether CM has finished updating yet, return false to stop
		// further reconcile operations on MC until CM is ready
		if clusterManager.Status.Phase != enterpriseApi.PhaseReady || cmImage != spec.Image {
			return false, nil
		}
		goto MonitoringConsole
	}
MonitoringConsole:
	if cr.GroupVersionKind().Kind == "MonitoringConsole" {

		namespacedName := types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetName()),
		}

		// check if the stateful set is created at this instance
		statefulSet := &appsv1.StatefulSet{}
		err := c.Get(ctx, namespacedName, statefulSet)
		if err != nil && k8serrors.IsNotFound(err) {
			return true, nil
		}

		mcImage, err := getCurrentImage(ctx, c, cr, SplunkMonitoringConsole)
		if err != nil {
			eventPublisher.Warning(ctx, "isMonitoringConsolerReadyForUpgrade", fmt.Sprintf("Could not get the Monitoring Console Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get monitoring console current image")
			return false, err
		}

		// check if an image upgrade is happening and whether CM has finished updating yet, return false to stop
		// further reconcile operations on MC until CM is ready
		if spec.Image != mcImage {
			return false, nil
		}

		return true, nil
	} else {

		// check if a MonitoringConsole is attached to the instance
		monitoringConsoleRef := spec.MonitoringConsoleRef
		if monitoringConsoleRef.Name == "" {
			goto SearchHeadCluster
		}

		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: monitoringConsoleRef.Name}
		monitoringConsole := &enterpriseApi.MonitoringConsole{}

		// get the monitoring console referred in search head cluster
		err := c.Get(ctx, namespacedName, monitoringConsole)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				goto SearchHeadCluster
			}
			eventPublisher.Warning(ctx, "isSearchHeadReadyForUpgrade", fmt.Sprintf("Could not find the Monitoring Console. Reason %v", err))
			scopedLog.Error(err, "Unable to get Monitoring Console")
			return false, err
		}

		mcImage, err := getCurrentImage(ctx, c, monitoringConsole, SplunkMonitoringConsole)
		if err != nil {
			eventPublisher.Warning(ctx, "isSearchHeadReadyForUpgrade", fmt.Sprintf("Could not get the Monitoring Console Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get Monitoring Console current image")
			return false, err
		}

		// check if an image upgrade is happening and whether the SearchHeadCluster is ready for the upgrade
		if monitoringConsole.Status.Phase != enterpriseApi.PhaseReady || mcImage != spec.Image {
			return false, nil
		}

		goto SearchHeadCluster
	}
SearchHeadCluster:
	if cr.GroupVersionKind().Kind == "SearchHeadCluster" {

		namespacedName := types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
		}

		// check if the stateful set is created at this instance
		statefulSet := &appsv1.StatefulSet{}
		err := c.Get(ctx, namespacedName, statefulSet)
		if err != nil && k8serrors.IsNotFound(err) {
			return true, nil
		}

		shcImage, err := getCurrentImage(ctx, c, cr, SplunkSearchHead)
		if err != nil {
			eventPublisher.Warning(ctx, "isSearchHeadReadyForUpgrade", fmt.Sprintf("Could not get the Search Head Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get Search Head current image")
			return false, err
		}

		// check if an image upgrade is happening and whether the SearchHeadCluster is ready for the upgrade
		if spec.Image != shcImage {
			return false, nil
		}
		return true, nil
	} else {

		// get the clusterManagerRef attached to the instance
		clusterManagerRef := spec.ClusterManagerRef

		// check if a search head cluster exists with the same ClusterManager instance attached
		searchHeadClusterInstance := enterpriseApi.SearchHeadCluster{}
		opts := []rclient.ListOption{
			rclient.InNamespace(cr.GetNamespace()),
		}
		searchHeadList, err := getSearchHeadClusterList(ctx, c, cr, opts)
		if err != nil {
			if err.Error() == "NotFound" {
				goto IndexerCluster
			}
			return false, err
		}
		if len(searchHeadList.Items) == 0 {
			goto IndexerCluster
		}

		// check if instance has the required ClusterManagerRef
		for _, shc := range searchHeadList.Items {
			if shc.Spec.ClusterManagerRef.Name == clusterManagerRef.Name {
				searchHeadClusterInstance = shc
				break
			}
		}
		if len(searchHeadClusterInstance.GetName()) == 0 {
			goto IndexerCluster
		}

		shcImage, err := getCurrentImage(ctx, c, &searchHeadClusterInstance, SplunkSearchHead)
		if err != nil {
			eventPublisher.Warning(ctx, "isIndexerClusterReadyForUpgrade", fmt.Sprintf("Could not get the Search Head Cluster Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get SearchHeadCluster current image")
			return false, err
		}

		idxImage, err := getCurrentImage(ctx, c, cr, SplunkIndexer)
		if err != nil {
			eventPublisher.Warning(ctx, "isIndexerClusterReadyForUpgrade", fmt.Sprintf("Could not get the Indexer Cluster Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get IndexerCluster current image")
			return false, err
		}

		// check if an image upgrade is happening and whether SHC has finished updating yet, return false to stop
		// further reconcile operations on IDX until SHC is ready
		if (spec.Image != idxImage) && (searchHeadClusterInstance.Status.Phase != enterpriseApi.PhaseReady || shcImage != spec.Image) {
			return false, nil
		}
		goto IndexerCluster
	}
IndexerCluster:
	if cr.GroupVersionKind().Kind == "IndexerCluster" {

		if mgr.c == nil {
			mgr.c = c
		}

		cm := mgr.getClusterManagerClient(ctx)
		clusterInfo, err := cm.GetClusterInfo(false)
		if err != nil {
			return false, fmt.Errorf("could not get cluster info from cluster manager")
		}
		if clusterInfo.MultiSite == "true" {
			opts := []rclient.ListOption{
				rclient.InNamespace(cr.GetNamespace()),
			}
			indexerList, err := getIndexerClusterList(ctx, c, cr, opts)
			if err != nil {
				return false, err
			}
			sortedList, err := getIndexerClusterSortedSiteList(ctx, c, spec.ClusterManagerRef, indexerList)

			preIdx := enterpriseApi.IndexerCluster{}

			for i, v := range sortedList.Items {
				if &v == cr {
					if i > 0 {
						preIdx = sortedList.Items[i-1]
					}
					break

				}
			}
			if len(preIdx.Name) != 0 {
				image, _ := getCurrentImage(ctx, c, &preIdx, SplunkIndexer)
				if preIdx.Status.Phase != enterpriseApi.PhaseReady || image != spec.Image {
					return false, nil
				}
			}

		}

		idxImage, err := getCurrentImage(ctx, c, cr, SplunkIndexer)
		if err != nil {
			eventPublisher.Warning(ctx, "isIndexerClusterReadyForUpgrade", fmt.Sprintf("Could not get the Indexer Cluster Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get IndexerCluster current image")
			return false, err
		}

		if spec.Image != idxImage {
			return false, nil
		}
		return true, nil
	} else {
		goto EndLabel
	}
EndLabel:
	return true, nil

}
