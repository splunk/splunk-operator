// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
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

// splunk-stub is a minimal HTTPS server that mimics just enough of the
// Splunk Enterprise REST API for the operator's reconciliation loops
// to reach PhaseReady without a real Splunk instance.
//
// Supported surface:
//   - GET  /                                 → 200 (readiness probe)
//   - GET  /services/cluster/manager/info    → canned "initialized + ready"
//   - GET  /services/cluster/manager/peers   → empty peer list
//   - GET  /services/shcluster/member/info   → canned "Up"
//   - GET  /services/shcluster/captain/info  → canned captain
//   - GET  /services/licenser/licenses       → empty license list
//   - POST /services/*                       → 200 (catch-all for restart, bundle push, etc.)
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"time"
)

func main() {
	mux := http.NewServeMux()

	// Readiness / liveness probe endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "splunk-stub OK")
	})

	// Cluster manager info — needed by IndexerCluster.updateStatus()
	mux.HandleFunc("/services/cluster/manager/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"entry": []map[string]interface{}{{
				"content": map[string]interface{}{
					"initialized_flag":     true,
					"indexing_ready_flag":  true,
					"service_ready_flag":   true,
					"maintenance_mode":     false,
					"rolling_restart_flag": false,
					"multisite":            false,
					"active_bundle":        map[string]string{"bundle_path": "/opt/splunk/etc/manager-apps/_cluster", "checksum": "stub"},
					"latest_bundle":        map[string]string{"bundle_path": "/opt/splunk/etc/manager-apps/_cluster", "checksum": "stub"},
				},
			}},
		})
	})

	// Cluster manager peers — empty list (no peers registered yet)
	mux.HandleFunc("/services/cluster/manager/peers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"entry": []interface{}{}})
	})

	// Cluster manager sites
	mux.HandleFunc("/services/cluster/manager/sites", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"entry": []interface{}{}})
	})

	// SHC member info
	mux.HandleFunc("/services/shcluster/member/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"entry": []map[string]interface{}{{
				"content": map[string]interface{}{
					"status":                         "Up",
					"is_registered":                  true,
					"active_historical_search_count": 0,
					"active_realtime_search_count":   0,
				},
			}},
		})
	})

	// SHC captain info
	mux.HandleFunc("/services/shcluster/captain/info", func(w http.ResponseWriter, r *http.Request) {
		hostname, _ := os.Hostname()
		writeJSON(w, map[string]interface{}{
			"entry": []map[string]interface{}{{
				"content": map[string]interface{}{
					"initialized_flag":     true,
					"rolling_restart_flag": false,
					"service_ready_flag":   true,
					"label":                hostname,
				},
			}},
		})
	})

	// License info
	mux.HandleFunc("/services/licenser/licenses", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"entry": []interface{}{}})
	})

	mux.HandleFunc("/services/licenser/groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"entry": []interface{}{}})
	})

	// Catch-all for any other /services/ endpoint (restart, bundle push, etc.)
	mux.HandleFunc("/services/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})

	cert, err := selfSignedCert()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate self-signed cert: %v\n", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    ":8089",
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}

	fmt.Println("splunk-stub listening on :8089 (HTTPS)")
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "splunk-stub"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", "*.svc.cluster.local"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}
