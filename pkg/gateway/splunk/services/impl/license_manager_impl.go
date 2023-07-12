package impl

import (
	"context"
	"net/http"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	clustermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster"
	lmmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license-manager"
	//logz "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// GetLicenseManagerPeers Access cluster node configuration details.
// endpoint: "https://localhost:8089/services/licenser/localpeer?output_mode=json"
func (p *splunkGateway) GetLicenseManagerPeers(context context.Context) (*[]lmmodel.LicenseLocalPeerEntry, error) {
	url := clustermodel.GetLicenseManagerLocalPeers

	// featch the license manager peer header into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &lmmodel.LicenseLocalPeerHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{
			"output_mode": "json",
		}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster license manager peers failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	return &envelop.Entry, err
}
