package impl

import (
	"context"
	"fmt"

	licensemodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license"
	provmodel "github.com/splunk/splunk-operator/pkg/provisioner/splunk/model"
	//"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var callLicenseLocalPeer = func(ctx context.Context, p *splunkProvisioner) (*[]licensemodel.LicenseLocalPeer, error) {
	lminfo, err := p.licensegateway.GetLicenseLocalPeer(ctx)
	if err != nil {
		return nil, err
	} else if lminfo == nil {
		return nil, fmt.Errorf("cluster manager info data is empty")
	}
	return lminfo, err
}

// GetClusterManagerStatus Access cluster node configuration details.
func (p *splunkProvisioner) GetLicenseLocalPeer(ctx context.Context, conditions *[]metav1.Condition) (result provmodel.Result, err error) {
	_, err = callLicenseLocalPeer(ctx, p)
	//peerlistptr, err := callLicenseLocalPeer(ctx, p)
	if err != nil {
		return result, err
	}
	/* else {
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
	}*/
	return result, err
}
