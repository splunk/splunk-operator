package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultRESTTimeout = 15 * time.Second

func (c *SplunkClient) newPOST(ctx context.Context, path string, data url.Values) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.ManagementURI, "/")+path,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.Username, c.Password)
	return req, nil
}

func (c *SplunkClient) doOK(req *http.Request) error {
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("splunk REST %s: %s", req.URL.Path, resp.Status)
	}
	return nil
}

func (c *SplunkClient) ReloadSSL(ctx context.Context) error {
	req, err := c.newPOST(ctx, "/services/server/control/reload_ssl_config", url.Values{})
	if err != nil {
		return err
	}
	return c.doOK(req)
}

func (c *SplunkClient) ReloadHEC(ctx context.Context) error {
	req, err := c.newPOST(ctx, "/services/data/inputs/http/ssl/_reload",
		url.Values{"requireServerRestart": {"true"}})
	if err != nil {
		return err
	}
	return c.doOK(req)
}

func (c *SplunkClient) RestartWebProxyOnly(ctx context.Context) error {
	req, err := c.newPOST(ctx, "/services/server/control/restart_webui_proxy_only", url.Values{})
	if err != nil {
		return err
	}
	return c.doOK(req)
}
