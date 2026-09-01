// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	operationPeersList         = "noah.peers.list"
	operationPeersGet          = "noah.peers.get"
	operationPeersDecommission = "noah.peers.decommission"
)

// Peer is the typed representation of one Noah peer.
type Peer struct {
	ID            string     `json:"id"`
	Data          PeerData   `json:"data"`
	Status        PeerStatus `json:"status"`
	LastHeartbeat int64      `json:"lastHeartbeat"`
	ScaleInStart  int64      `json:"scaleInStart,omitempty"`
}

func (peer Peer) validate() error {
	if peer.ID == "" {
		return fmt.Errorf("peer id is empty")
	}
	return nil
}

// PeerData contains incarnation and placement information reported by Noah.
type PeerData struct {
	ID                string `json:"id"`
	StartTime         int64  `json:"startTime"`
	Info              string `json:"info"`
	SiteID            string `json:"siteID,omitempty"`
	DecommissionReady string `json:"decommissionReady,omitempty"`
}

// PeerStatus is the lifecycle state reported by Noah for a peer.
type PeerStatus string

const (
	PeerStatusUnknown           PeerStatus = ""
	PeerStatusUp                PeerStatus = "up"
	PeerStatusDown              PeerStatus = "down"
	PeerStatusStarted           PeerStatus = "started"
	PeerStatusWarming           PeerStatus = "warming"
	PeerStatusWarmed            PeerStatus = "warmed"
	PeerStatusDecommissionReady PeerStatus = "decommission-ready"
	PeerStatusDecommissioning   PeerStatus = "decommissioning"
	PeerStatusDecommissioned    PeerStatus = "decommissioned"
)

func (client *Client) peersURL() string {
	return fmt.Sprintf("%s/%s/noah/v1/peers", client.endpoint, url.PathEscape(client.tenant))
}

// ListPeers returns all Noah peers visible in the configured tenant.
func (client *Client) ListPeers(ctx context.Context) ([]Peer, error) {
	var peers []Peer
	if err := client.do(ctx, operationPeersList, http.MethodGet, client.peersURL(), http.StatusOK, true, &peers); err != nil {
		return nil, err
	}
	if peers == nil {
		return nil, invalidResponse(operationPeersList, fmt.Errorf("peer list is null"))
	}
	for index, peer := range peers {
		if err := peer.validate(); err != nil {
			return nil, invalidResponse(operationPeersList, fmt.Errorf("peer %d: %w", index, err))
		}
	}
	return peers, nil
}

// GetPeer returns one exact Noah peer.
func (client *Client) GetPeer(ctx context.Context, peerID string) (*Peer, error) {
	if peerID == "" || strings.TrimSpace(peerID) != peerID {
		return nil, &Error{Operation: operationPeersGet, Kind: ErrorKindInvalidRequest}
	}
	requestURL := fmt.Sprintf("%s/%s", client.peersURL(), url.PathEscape(peerID))
	peer := &Peer{}
	if err := client.do(ctx, operationPeersGet, http.MethodGet, requestURL, http.StatusOK, true, peer); err != nil {
		return nil, err
	}
	if err := peer.validate(); err != nil {
		return nil, invalidResponse(operationPeersGet, err)
	}
	return peer, nil
}

// DecommissionAcknowledgement records that Noah accepted a decommission
// request. It is not evidence that decommission has completed.
type DecommissionAcknowledgement struct {
	PeerID string
}

// DecommissionPeer asks Noah to decommission one exact peer. A successful
// return means the request was accepted, not that decommission has completed.
func (client *Client) DecommissionPeer(ctx context.Context, peerID string) (*DecommissionAcknowledgement, error) {
	if peerID == "" || strings.TrimSpace(peerID) != peerID {
		return nil, &Error{Operation: operationPeersDecommission, Kind: ErrorKindInvalidRequest}
	}
	requestURL := fmt.Sprintf("%s/%s", client.peersURL(), url.PathEscape(peerID))
	// Until Noah defines an idempotency contract, a timeout or transport error
	// leaves the result of this mutation unknown and must not be auto-retried.
	if err := client.do(ctx, operationPeersDecommission, http.MethodDelete, requestURL, http.StatusAccepted, false, nil); err != nil {
		return nil, err
	}
	return &DecommissionAcknowledgement{PeerID: peerID}, nil
}
