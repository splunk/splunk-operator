package impl

import (
	"context"
	"net/http"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	servermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/server"
	healthmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/server/health"
)

// Shows the overall health of splunkd. The health of splunkd can be red, yellow, or green. The health of splunkd is based on the health of all features reporting to it.
// Authentication and Authorization:
//
//	Requires the admin role or list_health capability.
//
// Get health status of distributed deployment features.
// endpoint: https://<host>:<mPort>/services/server/health/deployment/details
func (p *splunkGateway) GetServerDeploymentHealthDetails(context context.Context) (*[]healthmodel.DeploymentContent, error) {
	url := healthmodel.DeploymentDetailsUrl

	// fetch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &healthmodel.DeploymentHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		Get(url)
	if err != nil {
		p.log.Error(err, "get deployment details failed")
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

	contentList := []healthmodel.DeploymentContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Shows the overall health of the splunkd health status tree, as well as each feature node and its respective color. For unhealthy nodes (non-green), the output includes reasons, indicators, thresholds, messages, and so on.
// Authentication and Authorization:
// 			Requires the admin role or list_health capability.
// /services/server/health/splunkd/details

func (p *splunkGateway) GetSplunkdHealthDetails(context context.Context) (*[]healthmodel.DeploymentContent, error) {
	url := healthmodel.SplunkdHealthDetailsUrl

	// fetch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &healthmodel.DeploymentHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		Get(url)
	if err != nil {
		p.log.Error(err, "get splunkd health details failed")
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

	contentList := []healthmodel.DeploymentContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Access information about the currently running Splunk instance.
// Note: This endpoint provides information on the currently running Splunk instance. Some values returned
// in the GET response reflect server status information. However, this endpoint is meant to provide
// information on the currently running instance, not the machine where the instance is running.
// Server status values returned by this endpoint should be considered deprecated and might not continue
// to be accessible from this endpoint. Use server/sysinfo to access server status instead.
//  endpoint: https://<host>:<mPort>/services/server/info

func (p *splunkGateway) GetServerInfo(context context.Context) (*[]healthmodel.DeploymentContent, error) {
	url := servermodel.InfoUrl

	// fetch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &healthmodel.DeploymentHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		Get(url)
	if err != nil {
		p.log.Error(err, "get splunkd health details failed")
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

	contentList := []healthmodel.DeploymentContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// List server/status child resources.
// endpoint: https://<host>:<mPort>/services/server/status

func (p *splunkGateway) GetServerStatus(context context.Context) (*[]healthmodel.DeploymentContent, error) {
	url := servermodel.StatusUrl

	// fetch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &healthmodel.DeploymentHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		Get(url)
	if err != nil {
		p.log.Error(err, "get splunkd health details failed")
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

	contentList := []healthmodel.DeploymentContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}
