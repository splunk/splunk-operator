package enterprise

import (
	"context"
	"errors"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	runtime "sigs.k8s.io/controller-runtime/pkg/client"
)

// DependencyNotReadyError represents normal asynchronous dependency
// convergence. Callers must translate it to Pending/Progressing status and a
// bounded requeue, not a terminal Error phase.
type DependencyNotReadyError struct {
	Kind          string
	Namespace     string
	Name          string
	Phase         enterpriseApi.Phase
	ObservedImage string
	DesiredImage  string
	Detail        string
}

func (e *DependencyNotReadyError) Error() string {
	message := fmt.Sprintf("Waiting for %s dependency %s/%s", e.Kind, e.Namespace, e.Name)
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", message, e.Detail)
	}
	if e.Phase != "" {
		message = fmt.Sprintf("%s (phase: %s)", message, e.Phase)
	}
	if e.ObservedImage != "" || e.DesiredImage != "" {
		message = fmt.Sprintf(
			"%s (current image: %s, desired image: %s)",
			message,
			e.ObservedImage,
			e.DesiredImage,
		)
	}
	return message
}

// AsDependencyNotReady returns the typed dependency wait carried by err.
func AsDependencyNotReady(err error) (*DependencyNotReadyError, bool) {
	var wait *DependencyNotReadyError
	if errors.As(err, &wait) {
		return wait, true
	}
	return nil, false
}

func dependencyWaitPhaseAndConditions(
	ctx context.Context,
	cr splcommon.MetaObject,
	existingConditions []metav1.Condition,
	isPaused bool,
	err error,
) (splcommon.PhaseAndConditions, bool) {
	wait, ok := AsDependencyNotReady(err)
	if !ok {
		return splcommon.PhaseAndConditions{}, false
	}

	message := wait.Error()
	logging.FromContext(ctx).InfoContext(
		ctx,
		"waiting for dependency convergence",
		"dependencyKind",
		wait.Kind,
		"dependencyNamespace",
		wait.Namespace,
		"dependencyName",
		wait.Name,
		"dependencyPhase",
		wait.Phase,
		"observedImage",
		wait.ObservedImage,
		"desiredImage",
		wait.DesiredImage,
	)
	GetEventPublisher(ctx, cr).Normal(ctx, EventReasonDependencyNotReady, message)

	return splcommon.SetPhaseAndConditions(existingConditions, splcommon.PhaseConditionInput{
		Phase:      enterpriseApi.PhasePending,
		IsPaused:   isPaused,
		Message:    message,
		Reason:     enterpriseApi.ReasonDependencyNotReady,
		Generation: cr.GetGeneration(),
	}), true
}

func dependencyNamespace(defaultNamespace string, ref corev1.ObjectReference) string {
	if ref.Namespace != "" {
		return ref.Namespace
	}
	return defaultNamespace
}

func newDependencyNotReady(
	kind,
	namespace,
	name string,
	phase enterpriseApi.Phase,
	observedImage,
	desiredImage,
	detail string,
) error {
	return &DependencyNotReadyError{
		Kind:          kind,
		Namespace:     namespace,
		Name:          name,
		Phase:         phase,
		ObservedImage: observedImage,
		DesiredImage:  desiredImage,
		Detail:        detail,
	}
}

func dependencyImageMismatchWait(
	kind,
	namespace,
	name,
	dependencyDesiredImage,
	dependentDesiredImage string,
	phase enterpriseApi.Phase,
) error {
	return newDependencyNotReady(
		kind,
		namespace,
		name,
		phase,
		dependencyDesiredImage,
		dependentDesiredImage,
		fmt.Sprintf(
			"dependency desired image %s does not match dependent desired image %s; waiting for coordinated desired state",
			dependencyDesiredImage,
			dependentDesiredImage,
		),
	)
}

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
	logger := logging.FromContext(ctx).With("func", "UpgradePathValidation", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, cr)

	kind := cr.GroupVersionKind().Kind
	logger.InfoContext(ctx, "kind is set to", "kind", kind)
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

		licenseManagerNamespace := dependencyNamespace(cr.GetNamespace(), licenseManagerRef)
		namespacedName := types.NamespacedName{Namespace: licenseManagerNamespace, Name: licenseManagerRef.Name}
		licenseManager := &enterpriseApi.LicenseManager{}
		// get the license manager referred in CR
		err := c.Get(ctx, namespacedName, licenseManager)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, newDependencyNotReady(
					"LicenseManager",
					licenseManagerNamespace,
					licenseManagerRef.Name,
					"",
					"",
					spec.Image,
					"referenced object does not exist yet",
				)
			}
			return false, err
		}

		if licenseManager.Spec.Image != "" && spec.Image != "" && licenseManager.Spec.Image != spec.Image {
			return false, dependencyImageMismatchWait(
				"LicenseManager",
				licenseManagerNamespace,
				licenseManagerRef.Name,
				licenseManager.Spec.Image,
				spec.Image,
				licenseManager.Status.Phase,
			)
		}

		if licenseManager.Status.Phase != enterpriseApi.PhaseReady {
			return false, newDependencyNotReady(
				"LicenseManager",
				licenseManagerNamespace,
				licenseManagerRef.Name,
				licenseManager.Status.Phase,
				"",
				spec.Image,
				"",
			)
		}

		// get current image of license manager
		lmImage, err := getCurrentImage(ctx, c, licenseManager, SplunkLicenseManager)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, newDependencyNotReady(
					"LicenseManager",
					licenseManagerNamespace,
					licenseManagerRef.Name,
					licenseManager.Status.Phase,
					"",
					spec.Image,
					"workload has not been created yet",
				)
			}
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not get the License Manager image — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get LicenseManager current image", "error", err)
			return false, err
		}
		if lmImage != spec.Image {
			return false, newDependencyNotReady(
				"LicenseManager",
				licenseManagerNamespace,
				licenseManagerRef.Name,
				licenseManager.Status.Phase,
				lmImage,
				spec.Image,
				"desired image has not reached the workload yet",
			)
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
			return false, err
		}
		return true, nil
	} else {
		// check if a cluster manager reference is added to custom resource
		clusterManagerRef := spec.ClusterManagerRef
		if clusterManagerRef.Name == "" {
			// if ref is not defined go to monitoring console step
			goto SearchHeadCluster
		}

		clusterManagerNamespace := dependencyNamespace(cr.GetNamespace(), clusterManagerRef)
		namespacedName := types.NamespacedName{Namespace: clusterManagerNamespace, Name: clusterManagerRef.Name}
		clusterManager := &enterpriseApi.ClusterManager{}

		// get the cluster manager referred in custom resource
		err := c.Get(ctx, namespacedName, clusterManager)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, newDependencyNotReady(
					"ClusterManager",
					clusterManagerNamespace,
					clusterManagerRef.Name,
					"",
					"",
					spec.Image,
					"referenced object does not exist yet",
				)
			}
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not read the Cluster Manager — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get ClusterManager", "error", err)
			return false, err
		}

		if clusterManager.Spec.Image != "" && spec.Image != "" && clusterManager.Spec.Image != spec.Image {
			return false, dependencyImageMismatchWait(
				"ClusterManager",
				clusterManagerNamespace,
				clusterManagerRef.Name,
				clusterManager.Spec.Image,
				spec.Image,
				clusterManager.Status.Phase,
			)
		}

		if clusterManager.Status.Phase != enterpriseApi.PhaseReady {
			return false, newDependencyNotReady(
				"ClusterManager",
				clusterManagerNamespace,
				clusterManagerRef.Name,
				clusterManager.Status.Phase,
				"",
				spec.Image,
				"",
			)
		}

		/// get the cluster manager image referred in custom resource
		cmImage, err := getCurrentImage(ctx, c, clusterManager, SplunkClusterManager)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, newDependencyNotReady(
					"ClusterManager",
					clusterManagerNamespace,
					clusterManagerRef.Name,
					clusterManager.Status.Phase,
					"",
					spec.Image,
					"workload has not been created yet",
				)
			}
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not get the Cluster Manager image — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get ClusterManager current image", "error", err)
			return false, err
		}

		// check if an image upgrade is happening and whether CM has finished updating yet, return false to stop
		// further reconcile operations on custom resource until CM is ready
		if cmImage != spec.Image {
			return false, newDependencyNotReady(
				"ClusterManager",
				clusterManagerNamespace,
				clusterManagerRef.Name,
				clusterManager.Status.Phase,
				cmImage,
				spec.Image,
				"desired image has not reached the workload yet",
			)
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
			opts := []runtime.ListOption{
				runtime.InNamespace(cr.GetNamespace()),
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
				if preIdx.Spec.Image != "" && spec.Image != "" && preIdx.Spec.Image != spec.Image {
					return false, dependencyImageMismatchWait(
						"IndexerCluster",
						preIdx.Namespace,
						preIdx.Name,
						preIdx.Spec.Image,
						spec.Image,
						preIdx.Status.Phase,
					)
				}
				if preIdx.Status.Phase != enterpriseApi.PhaseReady {
					return false, newDependencyNotReady(
						"IndexerCluster",
						preIdx.Namespace,
						preIdx.Name,
						preIdx.Status.Phase,
						"",
						spec.Image,
						"previous multisite peer group has not completed",
					)
				}
				image, imageErr := getCurrentImage(ctx, c, &preIdx, SplunkIndexer)
				if imageErr != nil {
					if k8serrors.IsNotFound(imageErr) {
						return false, newDependencyNotReady(
							"IndexerCluster",
							preIdx.Namespace,
							preIdx.Name,
							preIdx.Status.Phase,
							"",
							spec.Image,
							"previous multisite workload has not been created yet",
						)
					}
					return false, imageErr
				}
				if image != spec.Image {
					return false, newDependencyNotReady(
						"IndexerCluster",
						preIdx.Namespace,
						preIdx.Name,
						preIdx.Status.Phase,
						image,
						spec.Image,
						"previous multisite workload has not reached the desired image",
					)
				}
			}

		}
		return true, nil
	} else {
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
			return false, err
		}
		return true, nil
	} else {

		// get the clusterManagerRef attached to the instance
		clusterManagerRef := spec.ClusterManagerRef

		// check if a search head cluster exists with the same ClusterManager instance attached
		searchHeadClusterInstance := enterpriseApi.SearchHeadCluster{}
		opts := []runtime.ListOption{
			runtime.InNamespace(cr.GetNamespace()),
		}
		searchHeadList, err := getSearchHeadClusterList(ctx, c, cr, opts)
		if err != nil {
			if err.Error() == "NotFound" {
				goto MonitoringConsole
			}
			return false, err
		}
		if len(searchHeadList.Items) == 0 {
			goto MonitoringConsole
		}

		// check if instance has the ClusterManagerRef defined
		for _, shc := range searchHeadList.Items {
			if shc.Spec.ClusterManagerRef.Name == clusterManagerRef.Name {
				searchHeadClusterInstance = shc
				break
			}
		}
		if len(searchHeadClusterInstance.GetName()) == 0 {
			goto MonitoringConsole
		}

		if searchHeadClusterInstance.Spec.Image != "" && spec.Image != "" && searchHeadClusterInstance.Spec.Image != spec.Image {
			return false, dependencyImageMismatchWait(
				"SearchHeadCluster",
				searchHeadClusterInstance.Namespace,
				searchHeadClusterInstance.Name,
				searchHeadClusterInstance.Spec.Image,
				spec.Image,
				searchHeadClusterInstance.Status.Phase,
			)
		}

		if searchHeadClusterInstance.Status.Phase != enterpriseApi.PhaseReady {
			return false, newDependencyNotReady(
				"SearchHeadCluster",
				searchHeadClusterInstance.Namespace,
				searchHeadClusterInstance.Name,
				searchHeadClusterInstance.Status.Phase,
				"",
				spec.Image,
				"",
			)
		}

		shcImage, err := getCurrentImage(ctx, c, &searchHeadClusterInstance, SplunkSearchHead)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, newDependencyNotReady(
					"SearchHeadCluster",
					searchHeadClusterInstance.Namespace,
					searchHeadClusterInstance.Name,
					searchHeadClusterInstance.Status.Phase,
					"",
					spec.Image,
					"workload has not been created yet",
				)
			}
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not get the Search Head Cluster image — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get SearchHeadCluster current image", "error", err)
			return false, err
		}

		// check if an image upgrade is happening and whether SHC has finished updating yet, return false to stop
		// further reconcile operations on IDX until SHC is ready
		if shcImage != spec.Image {
			return false, newDependencyNotReady(
				"SearchHeadCluster",
				searchHeadClusterInstance.Namespace,
				searchHeadClusterInstance.Name,
				searchHeadClusterInstance.Status.Phase,
				shcImage,
				spec.Image,
				"desired image has not reached the workload yet",
			)
		}
		goto MonitoringConsole
	}
MonitoringConsole:
	if cr.GroupVersionKind().Kind == "MonitoringConsole" {

		listOpts := []runtime.ListOption{
			runtime.InNamespace(cr.GetNamespace()),
		}

		// get the list of cluster managers
		clusterManagerList := &enterpriseApi.ClusterManagerList{}
		err := c.List(ctx, clusterManagerList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not find the Cluster Manager list — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get ClusterManager list", "error", err)
			return false, err
		}

		// Run through list, if it has the MC reference, bail out if it is NOT ready
		for _, cm := range clusterManagerList.Items {
			if cm.Spec.MonitoringConsoleRef.Name == cr.GetName() {
				if cm.Status.Phase != enterpriseApi.PhaseReady {
					return false, newDependencyNotReady(
						"ClusterManager",
						cm.Namespace,
						cm.Name,
						cm.Status.Phase,
						"",
						spec.Image,
						"referenced tier must converge before MonitoringConsole upgrade",
					)
				}
			}
		}

		// get the list of search head clusters
		searchHeadClusterList := &enterpriseApi.SearchHeadClusterList{}
		err = c.List(ctx, searchHeadClusterList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not find the Search Head Cluster list — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get SearchHeadCluster list", "error", err)
			return false, err
		}

		// Run through list, if it has the MC reference, bail out if it is NOT ready
		for _, shc := range searchHeadClusterList.Items {
			if shc.Spec.MonitoringConsoleRef.Name == cr.GetName() {
				if shc.Status.Phase != enterpriseApi.PhaseReady {
					return false, newDependencyNotReady(
						"SearchHeadCluster",
						shc.Namespace,
						shc.Name,
						shc.Status.Phase,
						"",
						spec.Image,
						"referenced tier must converge before MonitoringConsole upgrade",
					)
				}
			}
		}

		// get the list of indexer clusters
		indexerClusterList := &enterpriseApi.IndexerClusterList{}
		err = c.List(ctx, indexerClusterList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not find the Indexer list — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get IndexerCluster list", "error", err)
			return false, err
		}

		// Run through list, if it has the MC reference, bail out if it is NOT ready
		for _, idx := range indexerClusterList.Items {
			if idx.Spec.MonitoringConsoleRef.Name == cr.GetName() {
				if idx.Status.Phase != enterpriseApi.PhaseReady {
					return false, newDependencyNotReady(
						"IndexerCluster",
						idx.Namespace,
						idx.Name,
						idx.Status.Phase,
						"",
						spec.Image,
						"referenced tier must converge before MonitoringConsole upgrade",
					)
				}
			}
		}

		// get the list of standalones
		standaloneList := &enterpriseApi.StandaloneList{}
		err = c.List(ctx, standaloneList, listOpts...)
		if err != nil && err.Error() != "NotFound" {
			eventPublisher.Warning(ctx, EventReasonUpgradeCheckFailed, "Could not find the Standalone list — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get Standalone list", "error", err)
			return false, err
		}

		// Run through list, if it has the MC reference, bail out if it is NOT ready
		for _, stdln := range standaloneList.Items {
			if stdln.Spec.MonitoringConsoleRef.Name == cr.GetName() {
				if stdln.Status.Phase != enterpriseApi.PhaseReady {
					return false, newDependencyNotReady(
						"Standalone",
						stdln.Namespace,
						stdln.Name,
						stdln.Status.Phase,
						"",
						spec.Image,
						"referenced tier must converge before MonitoringConsole upgrade",
					)
				}
			}
		}
		goto EndLabel
	}
EndLabel:
	return true, nil
}
