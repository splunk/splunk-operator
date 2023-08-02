package fixture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"path/filepath"

	//"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	licensemodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license"

	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/license-manager"
	model "github.com/splunk/splunk-operator/pkg/splunk/model"
	logz "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var log = logz.New().WithName("gateway").WithName("fixture")

// fixtureGateway implements the gateway.fixtureGateway interface
// and uses splunk to manage the host.
type fixtureGateway struct {
	// client for talking to splunk
	client *resty.Client
	// the splunk credentials
	credentials splunkmodel.SplunkCredentials
	// a logger configured for this host
	log logr.Logger
	// an event publisher for recording significant events
	publisher model.EventPublisher
	// state of the splunk
	state *Fixture
}

func findFixturePath() (string, error) {
	ext := ".env"
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		dir, err := os.Open(wd)
		if err != nil {
			fmt.Println("Error opening directory:", err)
			return "", err
		}
		defer dir.Close()

		files, err := dir.Readdir(-1)
		if err != nil {
			fmt.Println("Error reading directory:", err)
			return "", err
		}

		for _, file := range files {
			if file.Name() == ext {
				wd, err = filepath.Abs(wd)
				wd += "/pkg/gateway/splunk/license-manager/fixture/"
				return wd, err
			}
		}
		wd += "/.."
	}
}

// Fixture contains persistent state for a particular splunk instance
type Fixture struct {
}

// NewGateway returns a new Fixture Gateway
func (f *Fixture) NewGateway(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher model.EventPublisher) (gateway.Gateway, error) {
	p := &fixtureGateway{
		log:       log.WithValues("splunk", sad.Address),
		publisher: publisher,
		state:     f,
		client:    resty.New(),
	}
	return p, nil
}

func (p *fixtureGateway) GetLicenseGroup(ctx context.Context) (*[]licensemodel.LicenseGroup, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license_group.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicenseGroupUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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

func (p *fixtureGateway) GetLicense(ctx context.Context) (*[]licensemodel.License, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicenseUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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
		var content licensemodel.License
		s, err := json.Marshal(entry.Content)
		if err != nil {
			return &contentList, nil
		}
		//content = entry.Content.(licensemodel.License)
		err = json.Unmarshal([]byte(s), &content)
		if err != nil {
			return &contentList, nil
		}
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *fixtureGateway) GetLicenseLocalPeer(ctx context.Context) (*[]licensemodel.LicenseLocalPeer, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license_local_peer.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicenseLocalPeersUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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
		var content licensemodel.LicenseLocalPeer
		s, err := json.Marshal(entry.Content)
		if err != nil {
			return &contentList, nil
		}
		err = json.Unmarshal([]byte(s), &content)
		if err != nil {
			return &contentList, nil
		}
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *fixtureGateway) GetLicenseMessage(ctx context.Context) (*[]licensemodel.LicenseMessage, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license_message.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicenseMessagesUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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
		var content licensemodel.LicenseMessage
		s, err := json.Marshal(entry.Content)
		if err != nil {
			return &contentList, nil
		}
		err = json.Unmarshal([]byte(s), &content)
		if err != nil {
			return &contentList, nil
		}
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *fixtureGateway) GetLicensePools(ctx context.Context) (*[]licensemodel.LicensePool, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license_pools.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicensePoolsUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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
		var content licensemodel.LicensePool
		s, err := json.Marshal(entry.Content)
		if err != nil {
			return &contentList, nil
		}
		err = json.Unmarshal([]byte(s), &content)
		if err != nil {
			return &contentList, nil
		}
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *fixtureGateway) GetLicensePeers(context context.Context) (*[]licensemodel.LicensePeer, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license_peers.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicensePeersUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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
		var content licensemodel.LicensePeer
		s, err := json.Marshal(entry.Content)
		if err != nil {
			return &contentList, nil
		}
		err = json.Unmarshal([]byte(s), &content)
		if err != nil {
			return &contentList, nil
		}
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *fixtureGateway) GetLicenseUsage(ctx context.Context) (*[]licensemodel.LicenseUsage, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license_usage.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicenseUsageUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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
		var content licensemodel.LicenseUsage
		s, err := json.Marshal(entry.Content)
		if err != nil {
			return &contentList, nil
		}
		err = json.Unmarshal([]byte(s), &content)
		if err != nil {
			return &contentList, nil
		}
		contentList = append(contentList, content)
	}
	return &contentList, nil
}

func (p *fixtureGateway) GetLicenseStacks(ctx context.Context) (*[]licensemodel.LicenseStack, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/license_stack.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := licensemodel.GetLicenseStacksUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &licensemodel.LicenseHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
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
		var content licensemodel.LicenseStack
		s, err := json.Marshal(entry.Content)
		if err != nil {
			return &contentList, nil
		}
		err = json.Unmarshal([]byte(s), &content)
		if err != nil {
			return &contentList, nil
		}
		contentList = append(contentList, content)
	}
	return &contentList, nil
}
