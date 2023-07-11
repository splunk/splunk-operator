package impl

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/services"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// splunkProvisioner implements the provisioner.Provisioner interface
// and uses provisioner to manage the host.
type splunkProvisioner struct {
	// a logger configured for this host
	log logr.Logger
	// a debug logger configured for this host
	debugLog logr.Logger
	// an event publisher for recording significant events
	publisher gateway.EventPublisher
	// credentials
	credentials *splunkmodel.SplunkCredentials
	// gateway factory
	gateway gateway.Gateway
}

// var callGetClusterManagerInfo = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerInfoContent, error) {
// 	cminfo, err := p.gateway.GetClusterManagerInfo(ctx)
// 	if err != nil {
// 		return nil, err
// 	} else if cminfo == nil {
// 		return nil, fmt.Errorf("cluster manager info data is empty")
// 	}
// 	return cminfo, err
// }

var callGetClusterManagerHealth = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerHealthContent, error) {
	healthList, err := p.gateway.GetClusterManagerHealth(ctx)
	if err != nil {
		return nil, err
	} else if healthList == nil {
		return nil, fmt.Errorf("health data is empty")
	}
	return healthList, err
}

// var callGetClusterManagerSearchHeadStatus = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.SearchHeadContent, error) {
// 	sclist, err := p.gateway.GetClusterManagerSearchHeadStatus(ctx)
// 	if err != nil {
// 		return nil, err
// 	} else if sclist == nil {
// 		return nil, fmt.Errorf("search head list is empty")
// 	}
// 	return sclist, err
// }

// var callGetClusterManagerPeersStatus = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerPeerContent, error) {
// 	peerlist, err := p.gateway.GetClusterManagerPeers(ctx)
// 	if err != nil {
// 		return nil, err
// 	} else if peerlist == nil {
// 		return nil, fmt.Errorf("peer list is empty")
// 	}
// 	return peerlist, err
// }

// var callGetClusterManagerSitesStatus = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerPeerContent, error) {
// 	peerlist, err := p.gateway.GetClusterManagerPeers(ctx)
// 	if err != nil {
// 		return nil, err
// 	} else if peerlist == nil {
// 		return nil, fmt.Errorf("peer list is empty")
// 	}
// 	return peerlist, err
// }

// SetClusterManagerStatus Access cluster node configuration details.
func (p *splunkProvisioner) SetClusterManagerStatus(ctx context.Context, cr splcommon.MetaObject) error {

	// peerlistptr, err := callGetClusterManagerPeersStatus(ctx, p)
	// if err != nil {
	// 	return err
	// } else {
	// 	peerlist := *peerlistptr
	// 	for _, peer := range peerlist {
	// 		condition := metav1.Condition{
	// 			Type:    "Peers",
	// 			Message: fmt.Sprintf("%s with %s is %s ", peer.Site, peer.Label, peer.Status),
	// 			Reason:  peer.Site,
	// 		}
	// 		if peer.Status == "Up" {
	// 			condition.Status = metav1.ConditionTrue
	// 		} else {
	// 			condition.Status = metav1.ConditionFalse

	// 		}
	// 		// set condition to existing conditions list
	// 		meta.SetStatusCondition(conditions, condition)
	// 	}
	// }

	// cminfolistptr, err := callGetClusterManagerInfo(ctx, p)
	// if err != nil {
	// 	return err
	// }
	// cminfolist := *cminfolistptr
	// if cminfolist[0].Multisite {
	// 	var site string
	// 	multiSiteStatus := metav1.ConditionTrue
	// 	message := "multisite is up"
	// 	peerlist := *peerlistptr
	// 	for _, peer := range peerlist {
	// 		if !strings.Contains(peer.Status, "Up") {
	// 			site = peer.Site
	// 			multiSiteStatus = metav1.ConditionFalse
	// 			message = fmt.Sprintf("site %s with label %s status is %s", peer.Site, peer.Label, peer.Status)
	// 			break
	// 		} // set condition to existing conditions list
	// 	}
	// 	condition := metav1.Condition{
	// 		Type:    "Multisite",
	// 		Message: message,
	// 		Reason:  site,
	// 		Status:  multiSiteStatus,
	// 	}
	// 	meta.SetStatusCondition(conditions, condition)
	// }

	// business logic starts here
	//healthList, err := callGetClusterManagerHealth(ctx, p)
	healthList, err := callGetClusterManagerHealth(ctx, p)
	if err != nil {
		return err
	} else {
		hllist := *healthList
		// prepare fields for conditions
		for _, health := range hllist {
			if health.AllPeersAreUp == "1" {
				continue
			} else {
				cr.Status.Phase = enterprise.PhaseWarning
			}

			// set condition to existing conditions list

		}
	}

	return nil
}
