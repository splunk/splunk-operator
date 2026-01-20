package splunkd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client executes Splunkd actions via REST calls through port-forward.
type Client struct {
	Kube       *k8s.Client
	Namespace  string
	PodName    string
	Container  string
	SecretName string
	Username   string

	passwordMu     sync.Mutex
	passwordCached bool
	password       string
	passwordErr    error
}

// NewClient creates a Splunkd client for a target pod.
func NewClient(kube *k8s.Client, namespace, podName string) *Client {
	return &Client{
		Kube:      kube,
		Namespace: namespace,
		PodName:   podName,
		Username:  "admin",
	}
}

// WithContainer sets the target container.
func (c *Client) WithContainer(container string) *Client {
	c.Container = container
	return c
}

// WithSecretName returns a new client with the provided secret name.
func (c *Client) WithSecretName(secretName string) *Client {
	clone := NewClient(c.Kube, c.Namespace, c.PodName)
	clone.Container = c.Container
	clone.Username = c.Username
	clone.SecretName = secretName
	return clone
}

// WithPod returns a new client targeting a different pod.
func (c *Client) WithPod(podName string) *Client {
	clone := NewClient(c.Kube, c.Namespace, podName)
	clone.Container = c.Container
	clone.Username = c.Username
	clone.SecretName = c.SecretName
	return clone
}

// Exec runs a command in the target pod.
func (c *Client) Exec(ctx context.Context, cmd []string, stdin string) (string, string, error) {
	return c.Kube.Exec(ctx, c.Namespace, c.PodName, c.Container, cmd, stdin, false)
}

// CheckStatus verifies the Splunkd management endpoint is reachable.
func (c *Client) CheckStatus(ctx context.Context) error {
	_, err := c.doManagementRequest(ctx, http.MethodGet, "/services/server/info", url.Values{"output_mode": []string{"json"}}, nil)
	return err
}

// CreateIndex creates a new index via REST.
func (c *Client) CreateIndex(ctx context.Context, indexName string) error {
	form := url.Values{"name": []string{indexName}}
	_, err := c.doManagementRequest(ctx, http.MethodPost, "/services/data/indexes", url.Values{"output_mode": []string{"json"}}, form)
	return err
}

// CopyFile copies a local file into the pod.
func (c *Client) CopyFile(ctx context.Context, srcPath, destPath string) error {
	_, stderr, err := c.Kube.CopyFileToPod(ctx, c.Namespace, c.PodName, srcPath, destPath)
	if err != nil {
		return fmt.Errorf("copy file failed: %w (stderr=%s)", err, stderr)
	}
	return nil
}

// IngestOneshot ingests a file into an index via REST.
func (c *Client) IngestOneshot(ctx context.Context, filePath, indexName string) error {
	form := url.Values{
		"name":  []string{filePath},
		"index": []string{indexName},
	}
	_, err := c.doManagementRequest(ctx, http.MethodPost, "/services/data/inputs/oneshot", url.Values{"output_mode": []string{"json"}}, form)
	return err
}

// PerformSearchSync runs a synchronous search and returns raw JSON.
func (c *Client) PerformSearchSync(ctx context.Context, search string) (string, error) {
	form := url.Values{"search": []string{"search " + search}}
	body, err := c.doManagementRequest(ctx, http.MethodPost, "/services/search/jobs/export", url.Values{"output_mode": []string{"json"}}, form)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// PerformSearchReq starts an async search and returns a SID.
func (c *Client) PerformSearchReq(ctx context.Context, search string) (string, error) {
	form := url.Values{"search": []string{"search " + search}}
	body, err := c.doManagementRequest(ctx, http.MethodPost, "/services/search/jobs", url.Values{"output_mode": []string{"json"}}, form)
	if err != nil {
		return "", err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("search request unmarshal failed: %w", err)
	}
	sid, _ := payload["sid"].(string)
	if sid == "" {
		return "", fmt.Errorf("missing sid in response: %s", string(body))
	}
	return sid, nil
}

// GetSearchStatus retrieves async search status and returns true when done.
func (c *Client) GetSearchStatus(ctx context.Context, sid string) (bool, error) {
	body, err := c.doManagementRequest(ctx, http.MethodGet, fmt.Sprintf("/services/search/jobs/%s", sid), url.Values{"output_mode": []string{"json"}}, nil)
	if err != nil {
		return false, err
	}
	var payload searchJobStatusResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("search status unmarshal failed: %w", err)
	}
	if len(payload.Entries) == 0 {
		return false, fmt.Errorf("search status missing entries")
	}
	isDone, ok := payload.Entries[0].Content.IsDone.(bool)
	if ok {
		return isDone, nil
	}
	if raw, ok := payload.Entries[0].Content.IsDone.(string); ok {
		return raw == "1" || strings.EqualFold(raw, "true"), nil
	}
	return false, fmt.Errorf("unexpected isDone type: %T", payload.Entries[0].Content.IsDone)
}

// GetSearchResults retrieves async search results.
func (c *Client) GetSearchResults(ctx context.Context, sid string) (string, error) {
	body, err := c.doManagementRequest(ctx, http.MethodGet, fmt.Sprintf("/services/search/jobs/%s/results", sid), url.Values{"output_mode": []string{"json"}}, nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// AppInfo contains the app status details.
type AppInfo struct {
	Name     string
	Version  string
	Disabled bool
}

// GetAppInfo retrieves app metadata from the management API.
func (c *Client) GetAppInfo(ctx context.Context, appName string) (AppInfo, error) {
	path := fmt.Sprintf("/services/apps/local/%s", url.PathEscape(appName))
	body, err := c.doManagementRequest(ctx, http.MethodGet, path, url.Values{"output_mode": []string{"json"}}, nil)
	if err != nil {
		return AppInfo{}, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return AppInfo{}, fmt.Errorf("app info unmarshal failed: %w", err)
	}
	entries, _ := payload["entry"].([]interface{})
	if len(entries) == 0 {
		return AppInfo{}, fmt.Errorf("app info missing entry for %s", appName)
	}
	entry, _ := entries[0].(map[string]interface{})
	content, _ := entry["content"].(map[string]interface{})
	info := AppInfo{Name: appName}
	if name, ok := entry["name"].(string); ok && name != "" {
		info.Name = name
	}
	if content != nil {
		if version, ok := content["version"].(string); ok {
			info.Version = version
		}
		info.Disabled = parseBool(content["disabled"])
	}
	return info, nil
}

// CheckCredentials validates admin credentials with the management API.
func (c *Client) CheckCredentials(ctx context.Context, username, password string) error {
	_, err := c.doRequestWithAuth(ctx, 8089, http.MethodGet, "/services/server/info", url.Values{"output_mode": []string{"json"}}, nil, username, password, nil)
	return err
}

// SendHECEvent posts an event to the HTTP Event Collector.
func (c *Client) SendHECEvent(ctx context.Context, token string, event string) error {
	payload := fmt.Sprintf(`{"event":%q,"sourcetype":"manual"}`, event)
	headers := map[string]string{"Authorization": "Splunk " + token}
	_, err := c.doRequest(ctx, 8088, http.MethodPost, "/services/collector", nil, strings.NewReader(payload), headers, false, "", "")
	return err
}

// ManagementRequest issues a REST request against the Splunkd management port.
func (c *Client) ManagementRequest(ctx context.Context, method, path string, query url.Values, form url.Values) ([]byte, error) {
	return c.doManagementRequest(ctx, method, path, query, form)
}

func (c *Client) doManagementRequest(ctx context.Context, method, path string, query url.Values, form url.Values) ([]byte, error) {
	password, err := c.passwordForAuth(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRequestWithAuth(ctx, 8089, method, path, query, form, c.Username, password, nil)
}

func (c *Client) doRequestWithAuth(ctx context.Context, port int, method, path string, query url.Values, form url.Values, username, password string, headers map[string]string) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
		if headers == nil {
			headers = make(map[string]string)
		}
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	return c.doRequest(ctx, port, method, path, query, body, headers, true, username, password)
}

func (c *Client) doRequest(ctx context.Context, port int, method, path string, query url.Values, body io.Reader, headers map[string]string, useAuth bool, username, password string) ([]byte, error) {
	if c.Kube == nil {
		return nil, fmt.Errorf("kube client not configured")
	}
	forward, err := c.Kube.StartPortForward(ctx, c.Namespace, c.PodName, port)
	if err != nil {
		return nil, err
	}
	defer forward.Close()

	endpoint := fmt.Sprintf("https://127.0.0.1:%d%s", forward.LocalPort, path)
	if query != nil && len(query) > 0 {
		endpoint = endpoint + "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if useAuth {
		req.SetBasicAuth(username, password)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Note: InsecureSkipVerify is required for E2E testing because:
	// 1. Splunk pods use self-signed certificates by default
	// 2. This client connects via port-forward to localhost (127.0.0.1)
	// 3. Certificate hostname verification would fail for localhost connections
	// 4. This is test framework code, not production code
	// #nosec G402 -- This is intentional for E2E test framework connecting to self-signed Splunk certs
	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // Required for self-signed Splunk certs in E2E tests
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("splunkd request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

func parseBool(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "y":
			return true
		case "false", "0", "no", "n":
			return false
		}
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	}
	return false
}

func (c *Client) passwordForAuth(ctx context.Context) (string, error) {
	c.passwordMu.Lock()
	defer c.passwordMu.Unlock()
	if c.passwordCached {
		return c.password, c.passwordErr
	}
	if c.Kube == nil {
		c.passwordErr = fmt.Errorf("kube client not configured")
		c.passwordCached = true
		return "", c.passwordErr
	}
	secretName := strings.TrimSpace(c.SecretName)
	if secretName == "" {
		secretName = fmt.Sprintf("splunk-%s-secret", c.Namespace)
	}
	secret := &corev1.Secret{}
	err := c.Kube.Client.Get(ctx, client.ObjectKey{Namespace: c.Namespace, Name: secretName}, secret)
	if err != nil {
		c.passwordErr = err
		c.passwordCached = true
		return "", err
	}
	raw, ok := secret.Data["password"]
	if !ok || len(raw) == 0 {
		c.passwordErr = fmt.Errorf("secret %s missing password", secretName)
		c.passwordCached = true
		return "", c.passwordErr
	}
	c.password = string(raw)
	c.passwordCached = true
	return c.password, nil
}

type searchJobStatusResponse struct {
	Entries []searchJobStatusEntry `json:"entry"`
}

type searchJobStatusEntry struct {
	Content searchJobStatusContent `json:"content"`
}

type searchJobStatusContent struct {
	IsDone interface{} `json:"isDone"`
}
