package client

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// SplunkHTTPClient should already exist in your codebase.
// If not, it is: type SplunkHTTPClient interface { Do(*http.Request) (*http.Response, error) }

func NewInsecureHTTPClient() SplunkHTTPClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &http.Client{Transport: tr, Timeout: DefaultRESTTimeout}
}

// Build a client that targets a specific pod by IP on 8089
func NewPodClient(podIP, username, password string) *SplunkClient {
	return &SplunkClient{
		ManagementURI: "https://" + podIP + ":8089",
		Username:      username,
		Password:      password,
		Client:        NewInsecureHTTPClient(),
	}
}
