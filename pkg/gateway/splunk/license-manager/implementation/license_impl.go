package impl

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	licensemodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license"
	model "github.com/splunk/splunk-operator/pkg/splunk/model"
)

// splunkGateway implements the gateway.Gateway interface
// and uses gateway to manage the host.
type splunkGateway struct {
	// a logger configured for this host
	log logr.Logger
	// a debug logger configured for this host
	debugLog logr.Logger
	// an event publisher for recording significant events
	publisher model.EventPublisher
	// client for talking to splunk
	client *resty.Client
	// credentials
	credentials *splunkmodel.SplunkCredentials
}

func (p *splunkGateway) GetLicenseGroup(ctx context.Context) (*[]licensemodel.LicenseGroup, error) {
	url := licensemodel.GetLicenseGroupUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.LicenseGroup{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.LicenseGroup)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *splunkGateway) GetLicense(ctx context.Context) (*[]licensemodel.License, error) {
	url := licensemodel.GetLicenseUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.License{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.License)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *splunkGateway) GetLicenseLocalPeer(ctx context.Context) (*[]licensemodel.LicenseLocalPeer, error) {
	url := licensemodel.GetLicenseLocalPeersUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.LicenseLocalPeer{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.LicenseLocalPeer)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *splunkGateway) GetLicenseMessage(ctx context.Context) (*[]licensemodel.LicenseMessage, error) {
	url := licensemodel.GetLicenseMessagesUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.LicenseMessage{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.LicenseMessage)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *splunkGateway) GetLicensePools(ctx context.Context) (*[]licensemodel.LicensePool, error) {
	url := licensemodel.GetLicensePoolsUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.LicensePool{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.LicensePool)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *splunkGateway) GetLicensePeers(context context.Context) (*[]licensemodel.LicensePeer, error) {
	url := licensemodel.GetLicenseGroupUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.LicensePeer{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.LicensePeer)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *splunkGateway) GetLicenseUsage(ctx context.Context) (*[]licensemodel.LicenseUsage, error) {
	url := licensemodel.GetLicenseUsageUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.LicenseUsage{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.LicenseUsage)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *splunkGateway) GetLicenseStacks(ctx context.Context) (*[]licensemodel.LicenseStack, error) {
	url := licensemodel.GetLicenseStacksUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
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

	contentList := []licensemodel.LicenseStack{}
	for _, entry := range envelop.Entry {
		content := entry.Content.(licensemodel.LicenseStack)
		contentList = append(contentList, content)
	}
	return &contentList, nil
}
