// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// MockHTTPHandler is used to handle an HTTP request for a given URL
type MockHTTPHandler struct {
	Method string
	URL    string
	Status int
	Err    error
	Body   string
}

// MockHTTPClient is used to replicate an http.Client for unit tests
type MockHTTPClient struct {
	WantRequests []*http.Request
	GotRequests  []*http.Request
	Handlers     map[string]MockHTTPHandler
}

// getHandlerKey method for MockHTTPClient returns map key for a HTTP request
func (c *MockHTTPClient) getHandlerKey(req *http.Request) string {
	if req == nil {
		return ""
	}
	return fmt.Sprintf("%s %s", req.Method, req.URL.String())
}

// Do method for MockHTTPClient just tracks the requests that it receives
func (c *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.GotRequests = append(c.GotRequests, req)
	rsp, ok := c.Handlers[c.getHandlerKey(req)]
	if !ok {
		return nil, errors.New("NotFound")
	}
	httpResponse := http.Response{
		StatusCode: rsp.Status,
		Body:       ioutil.NopCloser(strings.NewReader(rsp.Body)),
	}
	return &httpResponse, rsp.Err
}

// AddHandler method for MockHTTPClient adds a wanted request and response to use for it
func (c *MockHTTPClient) AddHandler(req *http.Request, status int, body string, err error) {
	c.WantRequests = append(c.WantRequests, req)
	if c.Handlers == nil {
		c.Handlers = make(map[string]MockHTTPHandler)
	}
	c.Handlers[c.getHandlerKey(req)] = MockHTTPHandler{
		Method: req.Method,
		URL:    req.URL.String(),
		Status: status,
		Err:    err,
		Body:   body,
	}
}

// AddHandlers method for MockHTTPClient adds a wanted requests and responses
func (c *MockHTTPClient) AddHandlers(handlers ...MockHTTPHandler) {
	for n := range handlers {
		req, _ := http.NewRequest(handlers[n].Method, handlers[n].URL, nil)
		c.AddHandler(req, handlers[n].Status, handlers[n].Body, handlers[n].Err)
	}
}

// CheckRequests method for MockHTTPClient checks if requests received matches requests that we want
func (c *MockHTTPClient) CheckRequests(t *testing.T, testMethod string) {
	if len(c.GotRequests) != len(c.WantRequests) {
		t.Fatalf("%s got %d Requests; want %d", testMethod, len(c.GotRequests), len(c.WantRequests))
	}
	for n := range c.GotRequests {
		if !reflect.DeepEqual(c.GotRequests[n].URL.String(), c.WantRequests[n].URL.String()) {
			t.Errorf("%s GotRequests[%d]=%v; want %v", testMethod, n, c.GotRequests[n].URL.String(), c.WantRequests[n].URL.String())
		}
	}
}
