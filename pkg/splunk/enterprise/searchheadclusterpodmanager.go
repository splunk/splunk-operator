package enterprise

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// searchHeadClusterPodManager is used to manage the pods within a search head cluster
type searchHeadClusterPodManager struct {
	c               splcommon.ControllerClient
	cr              *enterpriseApi.SearchHeadCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// newSerachHeadClusterPodManager function to create pod manager this is added to write unit test case
var newSearchHeadClusterPodManager = func(client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) searchHeadClusterPodManager {
	return searchHeadClusterPodManager{
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               client,
	}
}

// Update for searchHeadClusterPodManager handles all updates for a statefulset of search heads
func (mgr *searchHeadClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	logger := logging.FromContext(ctx).With("func", "searchHeadClusterPodManager.Update")

	// Assign client
	if mgr.c == nil {
		mgr.c = c
	}

	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.cr)

	// Track last successful replica count to emit scale events after completion
	previousReadyReplicas := mgr.cr.Status.ReadyReplicas

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
		if termErr := splctrl.CheckPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
			logger.ErrorContext(ctx, "terminal pod failure detected; setting PhaseError", "error", termErr)
			return enterpriseApi.PhaseError, termErr
		}
		logger.InfoContext(ctx, "SearchHeadCluster is not ready", "error", err)
		// A scale up/down can already be underway even while captain election
		// is in flight (e.g. the member being recycled was the captain). Report
		// the transient phase from replica counts alone so callers don't see a
		// false Pending for the whole election window; PrepareScaleDown/Recycle
		// still won't run until CaptainReady returns true on a later reconcile.
		if mgr.cr.Status.ReadyReplicas > desiredReplicas {
			return enterpriseApi.PhaseScalingDown, nil
		}
		if mgr.cr.Status.ReadyReplicas > 0 && mgr.cr.Status.ReadyReplicas < desiredReplicas {
			return enterpriseApi.PhaseScalingUp, nil
		}
		return enterpriseApi.PhasePending, nil
	}

	// manage scaling and updates
	phase, err := splctrl.UpdateStatefulSetPods(ctx, mgr.c, statefulSet, mgr, desiredReplicas)
	if err != nil {
		return phase, err
	}

	// Emit scale events when phase is ready and ready replicas changed to match desired
	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas > previousReadyReplicas && mgr.cr.Status.ReadyReplicas == desiredReplicas {
			if eventPublisher != nil {
				eventPublisher.Normal(ctx, EventReasonScaledUp,
					fmt.Sprintf("Successfully scaled %s up from %d to %d replicas", mgr.cr.GetName(), previousReadyReplicas, desiredReplicas))
			}
		} else if desiredReplicas < previousReadyReplicas && mgr.cr.Status.ReadyReplicas == desiredReplicas {
			if eventPublisher != nil {
				eventPublisher.Normal(ctx, EventReasonScaledDown,
					fmt.Sprintf("Successfully scaled %s down from %d to %d replicas", mgr.cr.GetName(), previousReadyReplicas, desiredReplicas))
			}
		}
	}

	return phase, nil
}

// PrepareScaleDown for searchHeadClusterPodManager prepares search head pod to be removed via scale down event; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	logger := logging.FromContext(ctx).With("func", "PrepareScaleDown")
	// start by quarantining the pod
	result, err := mgr.PrepareRecycle(ctx, n)
	if err != nil || !result {
		return result, err
	}

	// pod is quarantined; decommission it
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
	logger.WarnContext(ctx, "member leaving SearchHeadCluster",
		"member", memberName,
		"remaining_count", len(mgr.cr.Status.Members)-1)

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
	logger := logging.FromContext(ctx).With("func", "PrepareRecycle")
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// Detain search head
		logger.InfoContext(ctx, "detaining SearchHeadCluster member", "memberName", memberName)
		c := mgr.getClient(ctx, n)

		podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, getApplicablePodNameForK8Probes(mgr.cr, n))

		err := setProbeLevelOnSplunkPod(ctx, podExecClient, livenessProbeLevelOne)

		if err != nil {
			// During the Recycle, our reconcile loop is entered multiple times. If the Pod is already down,
			// there is a chance of readiness probe failing, in which case, even the podExec will not be successful.
			// So, just log the message, and ignore the error.
			logger.WarnContext(ctx, "setting Probe level failed. Probably, the Pod is already down", "memberName", memberName)
		}

		logger.InfoContext(ctx, "initializes rolling upgrade process")
		err = c.InitiateUpgrade()

		if err != nil {
			logger.ErrorContext(ctx, "initialization of rolling upgrade failed", "error", err)
			return false, err
		}

		start := mgr.cr.Status.UpgradeStartTimestamp
		end := mgr.cr.Status.UpgradeEndTimestamp

		if end >= start {
			currentTime := time.Now().Unix()
			mgr.cr.Status.UpgradeStartTimestamp = currentTime

			metrics.UpgradeStartTime.Set(float64(currentTime))

			mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgrading
		}

		return false, c.SetSearchHeadDetention(true)

	case "ManualDetention":

		metrics.ActiveHistoricalSearchCount.With(prometheus.Labels{
			"sh_name": mgr.cr.Status.Members[n].Name,
		}).Set(float64(mgr.cr.Status.Members[n].ActiveHistoricalSearchCount))

		metrics.ActiveRealtimeSearchCount.With(prometheus.Labels{
			"sh_name": mgr.cr.Status.Members[n].Name,
		}).Set(float64(mgr.cr.Status.Members[n].ActiveRealtimeSearchCount))

		// Wait until active searches have drained
		searchesComplete := mgr.cr.Status.Members[n].ActiveHistoricalSearchCount+mgr.cr.Status.Members[n].ActiveRealtimeSearchCount == 0
		if searchesComplete {
			logger.InfoContext(ctx, "detention complete", "memberName", memberName)
		} else {
			logger.InfoContext(ctx, "waiting for active searches to complete", "memberName", memberName)
		}
		return searchesComplete, nil

	case "": // this can happen after the member has already been recycled and we're just waiting for state to update
		logger.InfoContext(ctx, "member has empty Status", "memberName", memberName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// FinishRecycle for searchHeadClusterPodManager completes recycle event for search head pod; it returns true when complete
func (mgr *searchHeadClusterPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	logger := logging.FromContext(ctx).With("func", "FinishRecycle")
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// not in detention
		return true, nil

	case "ManualDetention":
		// release from detention
		logger.InfoContext(ctx, "releasing SearchHeadCluster member from detention", "memberName", memberName)
		c := mgr.getClient(ctx, n)
		return false, c.SetSearchHeadDetention(false)

	case "": // member info is transiently unavailable (e.g. pod mid-restart); wait for it to come back
		logger.InfoContext(ctx, "member status is transiently unavailable, waiting for it to be reported again", "memberName", memberName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

func (mgr *searchHeadClusterPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	// check if shc is in an upgrade process
	if mgr.cr.Status.UpgradePhase == enterpriseApi.UpgradePhaseUpgrading {
		logger := logging.FromContext(ctx).With("func", "FinishUpgrade")
		c := mgr.getClient(ctx, n)

		// stop gathering metrics
		currentTime := time.Now().Unix()
		mgr.cr.Status.UpgradeEndTimestamp = currentTime

		metrics.UpgradeEndTime.Set(float64(currentTime))

		// revert upgrade state status
		mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgraded

		logger.InfoContext(ctx, "finalize Upgrade")
		return c.FinalizeUpgrade()
	}

	return nil
}

// getClient for searchHeadClusterPodManager returns a SplunkClient for the member n
func (mgr *searchHeadClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	logger := logging.FromContext(ctx).With("func", "searchHeadClusterPodManager.getClient")
	// Get Pod Name
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, splcommon.GetSplunkServiceName(SplunkSearchHead, mgr.cr.GetName(), true)))

	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		logger.ErrorContext(ctx, "couldn't retrieve the admin password from Pod", "member", memberName, "error", err)
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
	previousCaptain := mgr.cr.Status.Captain
	previousMemberCount := int32(len(mgr.cr.Status.Members))

	mgr.cr.Status.Captain = ""
	mgr.cr.Status.CaptainReady = false
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.cr.Status.ReadyReplicas == 0 {
		return nil
	}

	shcLogger := logging.FromContext(ctx)

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
			shcLogger.ErrorContext(ctx, "unable to retrieve SearchHeadCluster member info", "memberName", memberName, "error", err)
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

				if previousCaptain != "" && previousCaptain != captainInfo.Label {
					shcLogger.InfoContext(ctx, "captain election completed",
						"old_captain", previousCaptain,
						"new_captain", captainInfo.Label)
				}
			} else {
				mgr.cr.Status.CaptainReady = false
				shcLogger.ErrorContext(ctx, "captain election failed",
					"member", memberName,
					"error", err)
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

	newMemberCount := int32(len(mgr.cr.Status.Members))
	if newMemberCount > previousMemberCount {
		shcLogger.InfoContext(ctx, "member joined SearchHeadCluster",
			"total_members", newMemberCount,
			"previous_members", previousMemberCount)
	} else if newMemberCount < previousMemberCount {
		shcLogger.WarnContext(ctx, "member left SearchHeadCluster",
			"total_members", newMemberCount,
			"previous_members", previousMemberCount)
	}

	return nil
}
