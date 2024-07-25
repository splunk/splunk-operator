package enterprise

import (
	"context"
	"fmt"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	runtime "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// helps in mock function
var GetClusterInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager, mockCall bool) (*splclient.ClusterInfo, error) {
	cm := mgr.getClusterManagerClient(ctx)
	return cm.GetClusterInfo(false)
}

// UpgradePathValidation is used in validating if upgrade can be done to given custom resource
//
// the method follows the sequence
//  1. Standalone or License Manager
//  2. Cluster Manager - if LM ref is defined, wait for License manager to complete
//  3. Monitoring Console - if CM ref is defined, wait for Cluster Manager to complete
//  4. Search Head Cluster - if MC ref , CM ref , LM ref is defined, wait for them to complete in order,
//     if any one of them not defined, ignore them and wait for the one added in ref
//  5. Indexer Cluster - same as above also wait for search head cluster to complete before starting upgrade
//     if its multisite then do 1 site at a time
//     function returns bool and error , true  - go ahead with upgrade
//     false -  exit the reconciliation loop with error
func UpgradePathValidation(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec enterpriseApi.CommonSplunkSpec, mgr *indexerClusterPodManager) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isClusterManagerReadyForUpgrade").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)
	kind := cr.GroupVersionKind().Kind
	scopedLog.Info("kind is set to ", "kind", kind)
	// start from standalone first
	goto Standalone

	// if custom resource type is standalone or license manager go ahead and upgrade
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
		// if custom resource type not license manager or standalone then
		// check if there is license manager reference
		// if no reference go to cluster manager
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

		// get current image of license manager
		lmImage, err := getCurrentImage(ctx, c, licenseManager, SplunkLicenseManager)
		if err != nil {
			eventPublisher.Warning(ctx, "isClusterManagerReadyForUpgrade", fmt.Sprintf("Could not get the License Manager Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get licenseManager current image")
			return false, err
		}
		// if license manager status is ready and CR spec and current license manager image are not same
		// then return with error
		if licenseManager.Status.Phase != enterpriseApi.PhaseReady || lmImage != spec.Image {
			return false, fmt.Errorf("license manager is not ready or license manager current image is different than CR image")
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
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		}
		return true, nil
	} else {
		// check if a cluster manager reference is added to custom resource
		clusterManagerRef := spec.ClusterManagerRef
		if clusterManagerRef.Name == "" {
			// if ref is not defined go to monitoring console step
			goto SearchHeadCluster
		}

		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: clusterManagerRef.Name}
		clusterManager := &enterpriseApi.ClusterManager{}

		// get the cluster manager referred in custom resource
		err := c.Get(ctx, namespacedName, clusterManager)
		if err != nil {
			eventPublisher.Warning(ctx, "UpgradePathValidation", fmt.Sprintf("Could not find the Cluster Manager. Reason %v", err))
			scopedLog.Error(err, "Unable to get clusterManager")
			goto SearchHeadCluster
		}

		/// get the cluster manager image referred in custom resource
		cmImage, err := getCurrentImage(ctx, c, clusterManager, SplunkClusterManager)
		if err != nil {
			eventPublisher.Warning(ctx, "UpgradePathValidation", fmt.Sprintf("Could not get the Cluster Manager Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get clusterManager current image")
			return false, err
		}

		// check if an image upgrade is happening and whether CM has finished updating yet, return false to stop
		// further reconcile operations on custom resource until CM is ready
		if clusterManager.Status.Phase != enterpriseApi.PhaseReady || cmImage != spec.Image {
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
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return true, nil
			}
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

		// check if instance has the ClusterManagerRef defined
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
			eventPublisher.Warning(ctx, "UpgradePathValidation", fmt.Sprintf("Could not get the Search Head Cluster Image. Reason %v", err))
			scopedLog.Error(err, "Unable to get SearchHeadCluster current image")
			return false, err
		}

		// check if an image upgrade is happening and whether SHC has finished updating yet, return false to stop
		// further reconcile operations on IDX until SHC is ready
		if searchHeadClusterInstance.Status.Phase != enterpriseApi.PhaseReady || shcImage != spec.Image {
			return false, nil
		}
		goto IndexerCluster
	}
IndexerCluster:
	if cr.GroupVersionKind().Kind == "IndexerCluster" {

		// if manager client is not defined, then assign current client
		if mgr.c == nil {
			mgr.c = c
		}

		// check cluster info call using splunk rest api
		clusterInfo, err := GetClusterInfoCall(ctx, mgr, false)
		if err != nil {
			return false, fmt.Errorf("could not get cluster info from cluster manager")
		}
		// check if cluster is multisite
		if clusterInfo.MultiSite == "true" {
			opts := []rclient.ListOption{
				rclient.InNamespace(cr.GetNamespace()),
			}
			indexerList, err := getIndexerClusterList(ctx, c, cr, opts)
			if err != nil {
				return false, err
			}
			// get sorted current indexer site list
			sortedList, _ := getIndexerClusterSortedSiteList(ctx, c, spec.ClusterManagerRef, indexerList)

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
				// check if previous indexer have completed before starting next one
				image, _ := getCurrentImage(ctx, c, &preIdx, SplunkIndexer)
				if preIdx.Status.Phase != enterpriseApi.PhaseReady || image != spec.Image {
					return false, nil
				}
			}

		}
		goto MonitoringConsole
	} 
MonitoringConsole:
	if cr.GroupVersionKind().Kind == "MonitoringConsole" {

		listOpts := []runtime.ListOption{
			runtime.InNamespace(cr.GetNamespace()),
		}

		clusterManagerList := &enterpriseApi.ClusterManagerList{}
		// get the list cluster manager which have mc reference
		err := c.List(ctx, clusterManagerList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, "UpgradePathValidation", fmt.Sprintf("Could not find the Cluster Manager list. Reason %v", err))
			scopedLog.Error(err, "Unable to get clusterManager list")
			return false, err
		}
		for _, cm := range clusterManagerList.Items {
			if cm.Spec.MonitoringConsoleRef.Name == cr.GetName() {
				if cm.Status.Phase != enterpriseApi.PhaseReady {
					message := fmt.Sprintf("cluster manager %s is not ready", cm.Name)
					return false, fmt.Errorf(message)
				}
			}
		}

		searchHeadClusterList := &enterpriseApi.SearchHeadClusterList{}
		// get the list search head which have mc reference
		err = c.List(ctx, searchHeadClusterList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, "UpgradePathValidation", fmt.Sprintf("Could not find the Search Head Cluster list. Reason %v", err))
			scopedLog.Error(err, "Unable to get Search Head Cluster list")
			return false, err
		}

		for _, shc := range searchHeadClusterList.Items {
			if shc.Spec.MonitoringConsoleRef.Name == cr.GetName() {
				if shc.Status.Phase != enterpriseApi.PhaseReady {
					message := fmt.Sprintf("search head %s is not ready", shc.Name)
					return false, fmt.Errorf(message)
				}
			}
		}
		indexerClusterList := &enterpriseApi.IndexerClusterList{}
		// get the list indexer which have mc reference
		err = c.List(ctx, indexerClusterList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, "UpgradePathValidation", fmt.Sprintf("Could not find the Indexer list. Reason %v", err))
			scopedLog.Error(err, "Unable to get indexer list")
			return false, err
		}

		for _, idx := range indexerClusterList.Items {
			if idx.Name == cr.GetName() {
				if idx.Status.Phase != enterpriseApi.PhaseReady {
					message := fmt.Sprintf("indexer %s is not ready", idx.Name)
					return false, fmt.Errorf(message)
				}
			}
		}
		standaloneList := &enterpriseApi.IndexerClusterList{}
		// get the list standalone which have mc reference
		err = c.List(ctx, standaloneList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, "UpgradePathValidation", fmt.Sprintf("Could not find the Standalone list. Reason %v", err))
			scopedLog.Error(err, "Unable to get standalone list")
			return false, err
		}
		for _, stdln := range standaloneList.Items {
			if stdln.Name == cr.GetName() {
				if stdln.Status.Phase != enterpriseApi.PhaseReady {
					message := fmt.Sprintf("standalone %s is not ready", stdln.Name)
					return false, fmt.Errorf(message)
				}
			}
		}
		goto EndLabel
	}
EndLabel:
	return true, nil

}
