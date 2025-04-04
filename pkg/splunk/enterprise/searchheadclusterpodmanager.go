package enterprise

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	upgrade   "github.com/splunk/splunk-operator/pkg/splunk/enterprise/upgrade"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// searchHeadClusterPodManager is used to manage the pods within a search head cluster
type searchHeadClusterPodManager struct {
	c               splcommon.ControllerClient
	log             logr.Logger
	cr              *enterpriseApi.SearchHeadCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// newSerachHeadClusterPodManager function to create pod manager this is added to write unit test case
var newSearchHeadClusterPodManager = func(client splcommon.ControllerClient, log logr.Logger, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) searchHeadClusterPodManager {
	return searchHeadClusterPodManager{
		log:             log,
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               client,
	}
}

// GatherUpgradeMetrics collects upgrade metrics from each search head and updates Prometheus counters.
func (mgr *searchHeadClusterPodManager) GatherUpgradeMetrics(ctx context.Context) error {
	logger := log.FromContext(ctx)
	for i := int32(0); i < mgr.cr.Spec.Replicas; i++ {
		shName := fmt.Sprintf("sh-%d", i)
		client := mgr.getClient(ctx, i)
		upgradeMetrics, err := client.GetUpgradeSearchMetrics()
		if err != nil {
			logger.Error(err, "Error gathering upgrade metrics", "search head", shName)
			continue
		}
		ShortSearchSuccessCounter.WithLabelValues(shName).Add(float64(upgradeMetrics.ShortSearchSuccess))
		ShortSearchFailureCounter.WithLabelValues(shName).Add(float64(upgradeMetrics.ShortSearchFailure))
		LongSearchSuccessCounter.WithLabelValues(shName).Add(float64(upgradeMetrics.LongSearchSuccess))
		LongSearchFailureCounter.WithLabelValues(shName).Add(float64(upgradeMetrics.LongSearchFailure))
		TotalSearchSuccessCounter.WithLabelValues(shName).Add(float64(upgradeMetrics.TotalSearchSuccess))
		TotalSearchFailureCounter.WithLabelValues(shName).Add(float64(upgradeMetrics.TotalSearchFailure))
	}
	return nil
}

// todo: add logs, verbose, error handling
func (mgr *searchHeadClusterPodManager) UpdateUpgradePhase(ctx context.Context, phase string) error {
	// stub: add rest of the logic if needed
	mgr.cr.Status.UpgradePhase = phase
	return err
}

// HandleUpgrade follows a state machine based on the CR mgr.UpgradePhase.
// The phases are (in order):
//
//	NotStarted (or empty) → Initiated → DetentionSet → HistoricalSearchDrainComplete → Finalized → DetentionUnset → MetricsGathered
func (mgr *searchHeadClusterPodManager) HandleUpgrade(ctx context.Context) error {
	logger := log.FromContext(ctx)
	// Use the first search head's client for upgrade steps.
	client := mgr.getClient(ctx, 0)
	currentPhase := mgr.cr.Status.UpgradePhase

	// If UpgradePhase is empty or "NotStarted", start the upgrade.
	if currentPhase == "" || currentPhase == "NotStarted" {
		logger.Info("Upgrade phase is NotStarted, initiating upgrade")
		if err := client.InitShcUpgrade(); err != nil {
			return err
		}
		UpgradeStartTime.Set(float64(time.Now().Unix()))
		if err := mgr.UpdateUpgradePhase(ctx, mgr.newSplunkClient, mgr.cr, "Initiated"); err != nil {
			return err
		}
		currentPhase = "Initiated"
	}

	if currentPhase == "Initiated" {
		logger.Info("Setting manual detention mode")
		if err := client.SetManualDetentionMode("on"); err != nil {
			return err
		}
		if err := mgr.UpdateUpgradePhase(ctx, mgr.newSplunkClient, mgr.cr, "DetentionSet"); err != nil {
			return err
		}
		currentPhase = "DetentionSet"
	}

	if currentPhase == "DetentionSet" {
		logger.Info("Waiting for historical searches to finish")
		// Wait for historical searches to drain (using a 3-minute timeout in this example).
		if err := upgrade.WaitForHistoricalSearches(ctx, client, 3 * time.Minute); err != nil {
			return err
		}
		if err := mgr.UpdateUpgradePhase(ctx, mgr.newSplunkClient, mgr.cr, "HistoricalSearchDrainComplete"); err != nil {
			return err
		}
		currentPhase = "HistoricalSearchDrainComplete"
	}

	if currentPhase == "HistoricalSearchDrainComplete" {
		logger.Info("Finalizing SHC upgrade")
		if err := client.FinalizeShcUpgrade(); err != nil {
			return err
		}
		UpgradeEndTime.Set(float64(time.Now().Unix()))
		if err := mgr.UpdateUpgradePhase(ctx, mgr.newSplunkClient, mgr.cr, "Finalized"); err != nil {
			return err
		}
		currentPhase = "Finalized"
	}

	if currentPhase == "Finalized" {
		logger.Info("Unsetting manual detention mode after upgrade")
		if err := client.SetManualDetentionMode("off"); err != nil {
			return err
		}
		if err := mgr.UpdateUpgradePhase(ctx, mgr.newSplunkClient, mgr.cr, "DetentionUnset"); err != nil {
			return err
		}
		currentPhase = "DetentionUnset"
	}

	if currentPhase == "DetentionUnset" {
		logger.Info("Gathering upgrade metrics")
		if err := mgr.GatherUpgradeMetrics(ctx); err != nil {
			return err
		}
		if err := mgr.UpdateUpgradePhase(ctx, mgr.newSplunkClient, mgr.cr, "MetricsGathered"); err != nil {
			return err
		}
		currentPhase = "MetricsGathered"
	}

	// If the phase is "MetricsGathered", the upgrade is complete.
	if currentPhase == "MetricsGathered" {
		logger.Info("Upgrade process complete")
	}

	return nil
}

// Reconcile is the main reconciliation loop snippet.
// It calls HandleUpgrade if an upgrade scenario is active.
// TODO: delegate this Reconcile loop to job/controller pod instead by creating another SetupWithManager. Decouple
func (mgr *searchHeadClusterPodManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	// ... (other reconcile logic)

	instance := &enterpriseApi.SearchHeadCluster{}


	// If an upgrade is in progress (determined by CR mgr.UpgradePhase not being "MetricsGathered"),
	// then execute the upgrade state machine.
	if mgr.cr.Status.UpgradePhase != "MetricsGathered" {
		if err := mgr.HandleUpgrade(ctx); err != nil {
			logger.Error(err, "Upgrade handling failed")
			return err
		}
	}
	// ... (continue with remaining reconciliation)
	return nil
}

// Update for searchHeadClusterPodManager handles all updates for a statefulset of search heads
func (mgr *searchHeadClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	// Assign client
	if mgr.c == nil {
		mgr.c = c
	}

	// update statefulset, if necessary
	_, err := splctrl.ApplyStatefulSet(ctx, mgr.c, statefulSet)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}

	// for now pass the targetPodName as empty since we are going to fill it in ApplyShcSecret
	podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, "")

	// Check if a recycle of shc pods is necessary(due to shc_secret mismatch with namespace scoped secret)
	err = ApplyShcSecret(ctx, mgr, desiredReplicas, podExecClient)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}

	// update CR status with SHC information
	err = mgr.updateStatus(ctx, statefulSet)
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.CaptainReady {
		mgr.log.Info("Search head cluster is not ready", "reason ", err)
		return enterpriseApi.PhasePending, nil
	}

	// manage scaling and updates
	return splctrl.UpdateStatefulSetPods(ctx, mgr.c, statefulSet, mgr, desiredReplicas)
}

// PrepareScaleDown for searchHeadClusterPodManager prepares search head pod to be removed via scale down event; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	// start by quarantining the pod
	result, err := mgr.PrepareRecycle(ctx, n)
	if err != nil || !result {
		return result, err
	}

	// pod is quarantined; decommission it
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
	mgr.log.Info("Removing member from search head cluster", "memberName", memberName)
	c := mgr.getClient(ctx, n)
	err = c.RemoveSearchHeadClusterMember()
	if err != nil {
		return false, err
	}

	// all done -> ok to scale down the statefulset
	return true, nil
}

// PrepareRecycle for searchHeadClusterPodManager prepares search head pod to be recycled for updates; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// Detain search head
		mgr.log.Info("Detaining search head cluster member", "memberName", memberName)
		c := mgr.getClient(ctx, n)
		podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, getApplicablePodNameForK8Probes(mgr.cr, n))
		err := setProbeLevelOnSplunkPod(ctx, podExecClient, livenessProbeLevelOne)
		if err != nil {
			// During the Recycle, our reconcile loop is entered multiple times. If the Pod is already down,
			// there is a chance of readiness probe failing, in which case, even the podExec will not be successful.
			// So, just log the message, and ignore the error.
			mgr.log.Info("Setting Probe level failed. Probably, the Pod is already down", "memberName", memberName)
		}

		return false, c.SetSearchHeadDetention(true)

	case "ManualDetention":
		// Wait until active searches have drained
		searchesComplete := mgr.cr.Status.Members[n].ActiveHistoricalSearchCount+mgr.cr.Status.Members[n].ActiveRealtimeSearchCount == 0
		if searchesComplete {
			mgr.log.Info("Detention complete", "memberName", memberName)
		} else {
			mgr.log.Info("Waiting for active searches to complete", "memberName", memberName)
		}
		return searchesComplete, nil

	case "": // this can happen after the member has already been recycled and we're just waiting for state to update
		mgr.log.Info("Member has empty Status", "memberName", memberName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// FinishRecycle for searchHeadClusterPodManager completes recycle event for search head pod; it returns true when complete
func (mgr *searchHeadClusterPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// not in detention
		return true, nil

	case "ManualDetention":
		// release from detention
		mgr.log.Info("Releasing search head cluster member from detention", "memberName", memberName)
		c := mgr.getClient(ctx, n)
		return false, c.SetSearchHeadDetention(false)
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// getClient for searchHeadClusterPodManager returns a SplunkClient for the member n
func (mgr *searchHeadClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("searchHeadClusterPodManager.getClient").WithValues("name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())

	// Get Pod Name
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkSearchHead, mgr.cr.GetName(), true)))

	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		scopedLog.Error(err, "Couldn't retrieve the admin password from Pod")
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

// GetSearchHeadClusterMemberInfo used in mocking this function
var GetSearchHeadClusterMemberInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadClusterMemberInfo, error) {
	c := mgr.getClient(ctx, n)
	return c.GetSearchHeadClusterMemberInfo()
}

// GetSearchHeadCaptainInfo used in mocking this function
var GetSearchHeadCaptainInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadCaptainInfo, error) {
	c := mgr.getClient(ctx, n)
	return c.GetSearchHeadCaptainInfo()
}

// updateStatus for searchHeadClusterPodManager uses the REST API to update the status for a SearcHead custom resource
func (mgr *searchHeadClusterPodManager) updateStatus(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	// populate members status using REST API to get search head cluster member info
	mgr.cr.Status.Captain = ""
	mgr.cr.Status.CaptainReady = false
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.cr.Status.ReadyReplicas == 0 {
		return nil
	}
	gotCaptainInfo := false
	for n := int32(0); n < statefulSet.Status.Replicas; n++ {
		memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
		memberStatus := enterpriseApi.SearchHeadClusterMemberStatus{Name: memberName}
		memberInfo, err := GetSearchHeadClusterMemberInfo(ctx, mgr, n)
		if err == nil {
			memberStatus.Status = memberInfo.Status
			memberStatus.Adhoc = memberInfo.Adhoc
			memberStatus.Registered = memberInfo.Registered
			memberStatus.ActiveHistoricalSearchCount = memberInfo.ActiveHistoricalSearchCount
			memberStatus.ActiveRealtimeSearchCount = memberInfo.ActiveRealtimeSearchCount
		} else {
			mgr.log.Error(err, "Unable to retrieve search head cluster member info", "memberName", memberName)
		}

		if err == nil && !gotCaptainInfo {
			// try querying captain api; note that this should work on any node
			captainInfo, err := GetSearchHeadCaptainInfo(ctx, mgr, n)
			if err == nil {
				mgr.cr.Status.Captain = captainInfo.Label
				mgr.cr.Status.CaptainReady = captainInfo.ServiceReady
				mgr.cr.Status.Initialized = captainInfo.Initialized
				mgr.cr.Status.MinPeersJoined = captainInfo.MinPeersJoined
				mgr.cr.Status.MaintenanceMode = captainInfo.MaintenanceMode
				gotCaptainInfo = true
			} else {
				mgr.cr.Status.CaptainReady = false
				mgr.log.Error(err, "Unable to retrieve captain info", "memberName", memberName)
			}
		}

		if n < int32(len(mgr.cr.Status.Members)) {
			mgr.cr.Status.Members[n] = memberStatus
		} else {
			mgr.cr.Status.Members = append(mgr.cr.Status.Members, memberStatus)
		}
	}

	// truncate any extra members that we didn't check (leftover from scale down)
	if statefulSet.Status.Replicas < int32(len(mgr.cr.Status.Members)) {
		mgr.cr.Status.Members = mgr.cr.Status.Members[:statefulSet.Status.Replicas]
	}

	return nil
}
