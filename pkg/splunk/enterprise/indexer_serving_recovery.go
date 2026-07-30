// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const indexerHECBtoolCommand = `hec_btool_output="$(/opt/splunk/bin/splunk btool inputs list http 2>/dev/null)" || exit 1
printf '%s\n' "$hec_btool_output" | /usr/bin/awk '
    $1 == "[http]" { in_http=1; print; next }
    $1 ~ /^\[/ { if (in_http) exit; next }
    in_http && ($1 == "disabled" || $1 == "enableSSL" || $1 == "port") {
        print $1 " = " $3
    }
'`

type indexerHECServingConfig struct {
	enabled bool
	scheme  string
	port    int
}

var checkIndexerServingRecovery = func(
	ctx context.Context,
	mgr *indexerClusterPodManager,
	replacement *corev1.Pod,
) (bool, error) {
	return mgr.indexerServingRecoveryObserved(ctx, replacement)
}

// indexerServingRecoveryObserved proves more than local process readiness. The
// replacement must be published as a ready endpoint for the client-facing
// Indexer Service. If HEC is enabled, a separate healthy Splunk Pod must also
// reach the replacement's effective HEC health endpoint through Pod DNS.
//
// Running the request from a Splunk Pod works without an external ingress and
// lets a transparent service-mesh sidecar participate in the same Pod-to-Pod
// route. Mesh modes still require environment qualification. Ingress and
// external load-balancer routing cannot prove the identity of one exact
// replacement Pod.
func (mgr *indexerClusterPodManager) indexerServingRecoveryObserved(
	ctx context.Context,
	replacement *corev1.Pod,
) (bool, error) {
	published, err := mgr.indexerServicePublishesPod(ctx, replacement)
	if err != nil || !published {
		return false, err
	}

	hecConfig, err := mgr.readIndexerHECServingConfig(
		ctx,
		replacement.GetName(),
	)
	if err != nil {
		return false, err
	}
	observer, err := mgr.selectIndexerServingObserver(
		ctx,
		replacement.GetName(),
	)
	if err != nil || observer == "" {
		return false, err
	}
	targetHost := fmt.Sprintf(
		"%s.%s",
		replacement.GetName(),
		splcommon.GetServiceFQDN(
			mgr.cr.GetNamespace(),
			splcommon.GetSplunkServiceName(
				SplunkIndexer,
				mgr.cr.GetName(),
				true,
			),
		),
	)
	servingPath := ""
	servingPort := 0
	command := ""
	if hecConfig.enabled {
		servingPath = "hec"
		servingPort = hecConfig.port
		url := fmt.Sprintf(
			"%s://%s:%d/services/collector/health",
			hecConfig.scheme,
			targetHost,
			hecConfig.port,
		)
		command = fmt.Sprintf(
			"curl --silent --show-error --fail --insecure --noproxy '*' --connect-timeout 2 --max-time 3 '%s' >/dev/null",
			url,
		)
	} else {
		servingPath = "s2s"
		servingPort, err = indexerS2SContainerPort(replacement)
		if err != nil {
			return false, err
		}
		command = fmt.Sprintf(
			"/usr/bin/timeout 3 /usr/bin/bash -c 'exec 3<>/dev/tcp/%s/%d; exec 3>&-; exec 3<&-'",
			targetHost,
			servingPort,
		)
	}
	podExecClient := splutil.GetPodExecClient(
		mgr.c,
		mgr.cr,
		observer,
	)
	streamOptions := splutil.NewStreamOptionsObject(command)
	stdout, stderr, execErr := podExecClient.RunPodExecCommand(
		ctx,
		streamOptions,
		[]string{"/bin/sh"},
	)
	if execErr != nil {
		mgr.log.InfoContext(
			ctx,
			"waiting for remote Indexer serving recovery",
			"targetPod",
			replacement.GetName(),
			"observerPod",
			observer,
			"servingPath",
			servingPath,
			"port",
			servingPort,
			"stdout",
			stdout,
			"stderr",
			stderr,
			"error",
			execErr,
		)
		return false, nil
	}
	return true, nil
}

func indexerS2SContainerPort(pod *corev1.Pod) (int, error) {
	podName := ""
	if pod != nil {
		podName = pod.GetName()
		portName := GetPortName(s2sPort, protoTCP)
		for containerIndex := range pod.Spec.Containers {
			for portIndex := range pod.Spec.Containers[containerIndex].Ports {
				port := pod.Spec.Containers[containerIndex].Ports[portIndex]
				if port.Name == portName &&
					port.ContainerPort > 0 &&
					port.ContainerPort <= 65535 {
					return int(port.ContainerPort), nil
				}
			}
		}
	}
	return 0, fmt.Errorf(
		"Indexer Pod %s has no valid %s container port for remote serving recovery",
		podName,
		GetPortName(s2sPort, protoTCP),
	)
}

func (mgr *indexerClusterPodManager) indexerServicePublishesPod(
	ctx context.Context,
	pod *corev1.Pod,
) (bool, error) {
	endpointSlices := &discoveryv1.EndpointSliceList{}
	serviceName := splcommon.GetSplunkServiceName(
		SplunkIndexer,
		mgr.cr.GetName(),
		false,
	)
	if err := mgr.c.List(
		ctx,
		endpointSlices,
		client.InNamespace(mgr.cr.GetNamespace()),
		client.MatchingLabels{discoveryv1.LabelServiceName: serviceName},
	); err != nil {
		return false, fmt.Errorf(
			"list EndpointSlices for Indexer Service %s during replacement recovery: %w",
			serviceName,
			err,
		)
	}
	return endpointSlicesPublishReadyPod(endpointSlices.Items, pod), nil
}

func endpointSlicesPublishReadyPod(
	endpointSlices []discoveryv1.EndpointSlice,
	pod *corev1.Pod,
) bool {
	if pod == nil {
		return false
	}
	for sliceIndex := range endpointSlices {
		for endpointIndex := range endpointSlices[sliceIndex].Endpoints {
			endpoint := &endpointSlices[sliceIndex].Endpoints[endpointIndex]
			target := endpoint.TargetRef
			if target == nil || target.Name != pod.Name {
				continue
			}
			if pod.UID != "" &&
				target.UID != "" &&
				target.UID != pod.UID {
				continue
			}
			if endpoint.Conditions.Ready != nil &&
				*endpoint.Conditions.Ready {
				return true
			}
		}
	}
	return false
}

func (mgr *indexerClusterPodManager) readIndexerHECServingConfig(
	ctx context.Context,
	podName string,
) (indexerHECServingConfig, error) {
	podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, podName)
	streamOptions := splutil.NewStreamOptionsObject(indexerHECBtoolCommand)
	stdout, stderr, err := podExecClient.RunPodExecCommand(
		ctx,
		streamOptions,
		[]string{"/bin/sh"},
	)
	if err != nil {
		return indexerHECServingConfig{}, fmt.Errorf(
			"read effective HEC configuration from Indexer Pod %s: %w (stderr: %s)",
			podName,
			err,
			stderr,
		)
	}
	config, err := parseIndexerHECServingConfig(stdout)
	if err != nil {
		return indexerHECServingConfig{}, fmt.Errorf(
			"parse effective HEC configuration from Indexer Pod %s: %w",
			podName,
			err,
		)
	}
	return config, nil
}

func parseIndexerHECServingConfig(
	output string,
) (indexerHECServingConfig, error) {
	config := indexerHECServingConfig{
		scheme: "https",
		port:   8088,
	}
	inHTTPStanza := false
	httpStanzaSeen := false
	values := map[string]string{}
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inHTTPStanza = strings.EqualFold(line, "[http]")
			httpStanzaSeen = httpStanzaSeen || inHTTPStanza
			continue
		}
		if !inHTTPStanza {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		switch key {
		case "disabled", "enablessl", "port":
			values[key] = strings.ToLower(strings.TrimSpace(parts[1]))
		}
	}
	if !httpStanzaSeen {
		return config, nil
	}
	switch values["disabled"] {
	case "0", "false":
		config.enabled = true
	case "", "1", "true":
		return config, nil
	default:
		return config, fmt.Errorf(
			"unsupported [http] disabled value %q",
			values["disabled"],
		)
	}
	switch values["enablessl"] {
	case "0", "false":
		config.scheme = "http"
	case "", "1", "true":
	default:
		return indexerHECServingConfig{}, fmt.Errorf(
			"unsupported [http] enableSSL value %q",
			values["enablessl"],
		)
	}
	if values["port"] == "" {
		return config, nil
	}
	port, err := strconv.Atoi(values["port"])
	if err != nil || port < 1 || port > 65535 {
		return indexerHECServingConfig{}, fmt.Errorf(
			"invalid [http] port %q",
			values["port"],
		)
	}
	config.port = port
	return config, nil
}

func (mgr *indexerClusterPodManager) selectIndexerServingObserver(
	ctx context.Context,
	targetPod string,
) (string, error) {
	candidates := make([]string, 0, len(mgr.cr.Status.Peers)+1)
	for _, peer := range mgr.cr.Status.Peers {
		if peer.Name == targetPod ||
			peer.Status != "Up" ||
			!peer.Searchable {
			continue
		}
		candidates = append(candidates, peer.Name)
	}
	sort.Strings(candidates)

	if mgr.cr.Spec.ClusterManagerRef.Name != "" {
		candidates = append(
			candidates,
			GetSplunkStatefulsetPodName(
				SplunkClusterManager,
				mgr.cr.Spec.ClusterManagerRef.Name,
				0,
			),
		)
	} else if mgr.cr.Spec.ClusterMasterRef.Name != "" {
		candidates = append(
			candidates,
			GetSplunkStatefulsetPodName(
				SplunkClusterMaster,
				mgr.cr.Spec.ClusterMasterRef.Name,
				0,
			),
		)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, name := range candidates {
		if _, found := seen[name]; found {
			continue
		}
		seen[name] = struct{}{}
		pod := &corev1.Pod{}
		err := mgr.c.Get(
			ctx,
			types.NamespacedName{
				Namespace: mgr.cr.GetNamespace(),
				Name:      name,
			},
			pod,
		)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", err
		}
		if isKubernetesPodReady(pod) {
			return name, nil
		}
	}
	return "", nil
}
