package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	licensegateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/license-manager"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/services"
	provmodel "github.com/splunk/splunk-operator/pkg/provisioner/splunk/model"
	model "github.com/splunk/splunk-operator/pkg/splunk/model"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// splunkProvisioner implements the provisioner.Provisioner interface
// and uses provisioner to manage the host.
type splunkProvisioner struct {
	// a logger configured for this host
	log logr.Logger
	// a debug logger configured for this host
	debugLog logr.Logger
	// an event publisher for recording significant events
	publisher model.EventPublisher
	// credentials
	credentials *splunkmodel.SplunkCredentials
	// gateway factory
	gateway gateway.Gateway
	// splunk license factory
	licensegateway licensegateway.Gateway
}

var callGetClusterManagerInfo = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerInfoContent, error) {
	cminfo, err := p.gateway.GetClusterManagerInfo(ctx)
	if err != nil {
		return nil, err
	} else if cminfo == nil {
		return nil, fmt.Errorf("cluster manager info data is empty")
	}
	return cminfo, err
}

var callGetClusterManagerHealth = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerHealthContent, error) {
	healthList, err := p.gateway.GetClusterManagerHealth(ctx)
	if err != nil {
		return nil, err
	} else if healthList == nil {
		return nil, fmt.Errorf("health data is empty")
	}
	return healthList, err
}

var callGetClusterManagerStatus = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerStatusContent, error) {
	statuslist, err := p.gateway.GetClusterManagerStatus(ctx)
	if err != nil {
		return nil, err
	} else if statuslist == nil {
		return nil, fmt.Errorf("status list is empty")
	}
	return statuslist, err
}

var callGetClusterManagerPeersStatus = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerPeerContent, error) {
	peerlist, err := p.gateway.GetClusterManagerPeers(ctx)
	if err != nil {
		return nil, err
	} else if peerlist == nil {
		return nil, fmt.Errorf("peer list is empty")
	}
	return peerlist, err
}

var callGetClusterManagerSitesStatus = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerPeerContent, error) {
	peerlist, err := p.gateway.GetClusterManagerPeers(ctx)
	if err != nil {
		return nil, err
	} else if peerlist == nil {
		return nil, fmt.Errorf("peer list is empty")
	}
	return peerlist, err
}

// GetClusterManagerStatus Access cluster node configuration details.
func (p *splunkProvisioner) GetClusterManagerStatus(ctx context.Context, conditions *[]metav1.Condition) (result provmodel.Result, err error) {

	peerlistptr, err := callGetClusterManagerPeersStatus(ctx, p)
	if err != nil {
		return result, err
	} else {
		peerlist := *peerlistptr
		for _, peer := range peerlist {
			condition := metav1.Condition{
				Type:    peer.Label,
				Message: fmt.Sprintf("%s in site %s is %s ", peer.Label, peer.Site, peer.Status),
				Reason:  peer.Site,
			}
			if peer.Status == "Up" {
				condition.Status = metav1.ConditionTrue
			} else {
				condition.Status = metav1.ConditionFalse

			}
			// set condition to existing conditions list
			meta.SetStatusCondition(conditions, condition)
		}
	}

	cminfolistptr, err := callGetClusterManagerInfo(ctx, p)
	if err != nil {
		return result, err
	}
	cminfolist := *cminfolistptr
	if cminfolist[0].Multisite {
		var site string
		multiSiteStatus := metav1.ConditionTrue
		message := "multisite is up"
		peerlist := *peerlistptr
		for _, peer := range peerlist {
			if !strings.Contains(peer.Status, "Up") {
				site = peer.Site
				multiSiteStatus = metav1.ConditionFalse
				message = fmt.Sprintf("site %s with label %s status is %s", peer.Site, peer.Label, peer.Status)
				break
			} // set condition to existing conditions list
		}
		condition := metav1.Condition{
			Type:    "Multisite",
			Message: message,
			Reason:  site,
			Status:  multiSiteStatus,
		}
		meta.SetStatusCondition(conditions, condition)
	}

	healthList, err := callGetClusterManagerHealth(ctx, p)
	if err != nil {
		return result, err
	} else {
		hllist := *healthList
		// prepare fields for conditions
		for _, health := range hllist {
			condition := metav1.Condition{
				Type:    "Health",
				Message: "all the peers of indexer cluster status",
				Reason:  "PeersStatus",
			}
			if health.AllPeersAreUp == "1" {
				condition.Status = metav1.ConditionTrue
			} else {
				condition.Status = metav1.ConditionFalse
			}
			// set condition to existing conditions list
			meta.SetStatusCondition(conditions, condition)
		}
	}
	result.Dirty = true
	return result, err
}

// CheckClusterManagerHealth
func (p *splunkProvisioner) CheckClusterManagerHealth(ctx context.Context) (result provmodel.Result, err error) {
	return result, nil
}

func (p *splunkProvisioner) SetClusterInMaintenanceMode(ctx context.Context, mode bool) error {
	return p.gateway.SetClusterInMaintenanceMode(ctx, mode)
}

func (p *splunkProvisioner) IsClusterInMaintenanceMode(ctx context.Context) (bool, error) {
	return p.gateway.IsClusterInMaintenanceMode(ctx)
}
