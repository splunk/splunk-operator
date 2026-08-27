// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package telapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

const (
	splunkSearchHead = "search-head"

	statefulSetPodTemplateStr = "splunk-%s-%s-%d"

	applySHCBundleCmdStr = "/opt/splunk/bin/splunk apply shcluster-bundle -target https://%s:8089 -auth admin:%s --answer-yes -push-default-apps true &> %s &"

	shcAppsLocationOnDeployer = "/opt/splunk/etc/shcluster/apps/"

	telAppConfString = `[install]
is_configured = 1

[ui]
is_visible = 0
label = Splunk Operator for K8s

[launcher]
author = Splunk
description = When telemetry is enabled, this app is used to help Splunk understand how many customers are deploying Splunk using Splunk Operator for K8s
version = 1.0.0
`

	telAppDefMetaConfString = `[]
access = read : [ * ], write : [ admin ]
`

	createTelAppNonShcString = "mkdir -p /opt/splunk/etc/apps/app_tel_for_sok/default/; mkdir -p /opt/splunk/etc/apps/app_tel_for_sok/metadata/; printf '%%s' \"%s\" > /opt/splunk/etc/apps/app_tel_for_sok/default/app.conf; printf '%%s' \"%s\" > /opt/splunk/etc/apps/app_tel_for_sok/metadata/default.meta"
	createTelAppShcString    = "mkdir -p %s/app_tel_for_sok/default/; mkdir -p %s/app_tel_for_sok/metadata/; printf '%%s' \"%s\" > %s/app_tel_for_sok/default/app.conf; printf '%%s' \"%s\" > %s/app_tel_for_sok/metadata/default.meta"

	telAppReloadString = "curl -k -u admin:%s https://localhost:8089/services/apps/local/_reload"
)

// AddTelApp adds a telemetry app.
var AddTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
	logger := logging.FromContext(ctx).With("func", "AddTelApp",
		"name", cr.GetObjectMeta().GetName(),
		"namespace", cr.GetObjectMeta().GetNamespace())

	crKind := cr.GetObjectKind().GroupVersionKind().Kind

	adminPwd, err := splutil.GetAdminPasswordFromNamespaceScopedSecret(ctx, podExecClient.GetClient(), cr.GetNamespace())
	if err != nil {
		logger.ErrorContext(ctx, "failed to retrieve admin password", "error", err)
		return err
	}

	var command1, command2 string
	if crKind != "SearchHeadCluster" {
		command1 = fmt.Sprintf(createTelAppNonShcString, telAppConfString, telAppDefMetaConfString)
		command2 = fmt.Sprintf(telAppReloadString, shellQuote(adminPwd))
	} else {
		command1 = fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, shcAppsLocationOnDeployer, telAppConfString, shcAppsLocationOnDeployer, telAppDefMetaConfString, shcAppsLocationOnDeployer)
		command2 = fmt.Sprintf(applySHCBundleCmdStr, getSplunkStatefulsetURL(cr.GetNamespace(), splunkSearchHead, cr.GetName(), 0, false), shellQuote(adminPwd), "/tmp/status.txt")
	}

	err = runCustomCommandOnSplunkPods(ctx, cr, replicas, command1, adminPwd, podExecClient)
	if err != nil {
		logger.ErrorContext(ctx, "unable to run command on splunk pod", "error", err)
		return err
	}

	err = runCustomCommandOnSplunkPods(ctx, cr, replicas, command2, adminPwd, podExecClient)
	if err != nil {
		logger.ErrorContext(ctx, "unable to run command on splunk pod", "error", err)
		return err
	}

	return err
}

func runCustomCommandOnSplunkPods(ctx context.Context, cr splcommon.MetaObject, replicas int32, command string, adminPwd string, podExecClient splutil.PodExecClientImpl) error {
	var err error
	var stdOut string

	streamOptions := splutil.NewStreamOptionsObject(command)
	for replicaIndex := 0; replicaIndex < int(replicas); replicaIndex++ {
		podName := getApplicablePodName(cr, replicaIndex)
		podExecClient.SetTargetPodName(ctx, podName)

		splutil.ResetStringReader(streamOptions, command)

		stdOut, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
		if err != nil {
			err = fmt.Errorf("unable to run command %s. stdout: %s, err: %s", redactSplunkAuth(command, adminPwd), stdOut, err)
			break
		}
	}
	return err
}

func getApplicablePodName(cr splcommon.MetaObject, ordinalIdx int) string {
	var podType string

	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		podType = "standalone"
	case "LicenseManager":
		podType = "license-manager"
	case "LicenseMaster":
		podType = "license-master"
	case "SearchHeadCluster":
		podType = "deployer"
	case "IndexerCluster":
		return ""
	case "ClusterMaster":
		podType = "cluster-master"
	case "ClusterManager":
		podType = "cluster-manager"
	case "MonitoringConsole":
		podType = "monitoring-console"
	case "IngestorCluster":
		podType = "ingestor"
	}

	return fmt.Sprintf(statefulSetPodTemplateStr, cr.GetName(), podType, ordinalIdx)
}

func getTelAppNameExtension(crKind string) (string, error) {
	switch crKind {
	case "Standalone":
		return "stdaln", nil
	case "LicenseMaster":
		return "lmaster", nil
	case "LicenseManager":
		return "lmanager", nil
	case "SearchHeadCluster":
		return "shc", nil
	case "ClusterMaster":
		return "cmaster", nil
	case "ClusterManager":
		return "cmanager", nil
	case "IngestorCluster":
		return "ingestor", nil
	default:
		return "", errors.New("Invalid CR kind for telemetry app")
	}
}

func getSplunkStatefulsetURL(namespace string, instanceType string, identifier string, index int32, hostnameOnly bool) string {
	podName := fmt.Sprintf(statefulSetPodTemplateStr, identifier, instanceType, index)
	if hostnameOnly {
		return podName
	}

	return splcommon.GetServiceFQDN(namespace,
		fmt.Sprintf(
			"%s.%s",
			podName,
			splcommon.GetSplunkServiceName(splcommon.InstanceType(instanceType), identifier, true),
		))
}

// shellQuote wraps s in single quotes for safe shell interpolation.
// Embedded single quotes are escaped using the sequence: quote, backslash, quote, quote.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// redactSplunkAuth replaces raw and shell-quoted adminPwd in cmd with **** for safe logging.
func redactSplunkAuth(cmd, adminPwd string) string {
	if adminPwd == "" {
		return cmd
	}

	redacted := strings.ReplaceAll(cmd, shellQuote(adminPwd), "****")
	return strings.ReplaceAll(redacted, adminPwd, "****")
}
