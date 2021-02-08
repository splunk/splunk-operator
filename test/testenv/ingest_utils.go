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

package testenv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	// Used to move files between pods
	_ "unsafe"

	// Import kubectl cmd cp utils
	_ "k8s.io/kubernetes/pkg/kubectl/cmd/cp"
)

// CreateMockLogfile creates a mock logfile with n entries to be ingested.
//   - Each logline has a random number that can be searched after ingest.
//   - Timestamps for the logs are set to a line a second ending with the current time
func CreateMockLogfile(logFile string, totalLines int) error {
	// Create data log
	var file, err = os.Create(logFile)
	if err != nil {
		logf.Log.Error(err, "Failed File Created", "logFile", logFile)
		return err
	}
	logf.Log.Info("File Created Successfully", "logFile", logFile)

	// Write some text line-by-line to file.
	var logLine strings.Builder
	level := "DEBUG"
	component := "GenericComponent"
	msg := "This log line is special!"
	timestamp := time.Now()

	// Simulate a log every second.  This could be adjusted however for this simple case its probably sufficient
	timestamp = timestamp.Add(time.Second * time.Duration(-totalLines))
	rand.Seed(time.Now().UnixNano())

	// Write each line to the file
	for i := 0; i < totalLines; i++ {
		fmt.Fprintf(&logLine, "%s %s %s %s randomNumber=%d\n", timestamp.Format("01-02-2006 15:04:05.000"), level, component, msg, rand.Int63())
		_, err = file.WriteString(logLine.String())
		if err != nil {
			logf.Log.Error(err, "Failed File Write", "logFile", logFile, "logLine", logLine.String())
			return err
		}
		timestamp = timestamp.Add(time.Second)
	}

	// Save logFile
	err = file.Sync()
	if err != nil {
		logf.Log.Error(err, "Failed File Save", "logFile", logFile, "logLine", logLine)
		return err
	}
	logf.Log.Info("File Updated Successfully", "logFile", logFile)

	return nil
}

// CreateAnIndexStandalone creates an index on a standalone instance using the CLI
func CreateAnIndexStandalone(indexName string, podName string, deployment *Deployment) error {

	var addIndexCmd strings.Builder
	splunkBin := "/opt/splunk/bin/splunk"
	username := "admin"
	password := "$(cat /mnt/splunk-secrets/password) "
	splunkCmd := "add index"

	fmt.Fprintf(&addIndexCmd, "%s %s %s -auth %s:%s", splunkBin, splunkCmd, indexName, username, password)
	command := []string{"/bin/bash"}
	stdin := addIndexCmd.String()
	addIndexResp, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "stdin", stdin, "addIndexResp", addIndexResp, "stderr", stderr)
		return err
	}

	// Validate the response of the CLI command
	var expectedResp strings.Builder
	fmt.Fprintf(&expectedResp, "Index \"%s\" added.", indexName)
	if strings.Compare(addIndexResp, expectedResp.String()) == 0 {
		logf.Log.Error(err, "Failed response to add index to splunk", "pod", podName, "addIndexResp", addIndexResp)
		return errors.New("Failed response to add index to splunk")
	}

	logf.Log.Info("Added index to Splunk", "podName", podName, "addIndexResp", addIndexResp)
	return nil
}

// IngestFileViaOneshot ingests a file into an instance using the oneshot CLI
func IngestFileViaOneshot(logFile string, indexName string, podName string, deployment *Deployment) error {

	// Send it to the instance
	resp, stderr, cpErr := CopyFileToPod(podName, logFile, logFile, deployment)
	if cpErr != nil {
		logf.Log.Error(cpErr, "Failed File Copy to pod", "logFile", logFile, "podName", podName, "stderr", stderr)
		return cpErr
	}
	logf.Log.Info("File Copied Successfully", "logFile", logFile, "resp", resp)

	// oneshot log into specified index
	var addOneshotCmd strings.Builder
	splunkBin := "/opt/splunk/bin/splunk"
	username := "admin"
	password := "$(cat /mnt/splunk-secrets/password) "
	splunkCmd := "add oneshot"

	fmt.Fprintf(&addOneshotCmd, "%s %s %s -index %s -auth %s:%s", splunkBin, splunkCmd, logFile, indexName, username, password)
	command := []string{"/bin/bash"}
	stdin := addOneshotCmd.String()
	addOneshotResp, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "stdin", stdin, "addOneshotResp", addOneshotResp, "stderr", stderr)
		return err
	}

	// Validate the expected CLI response
	var expectedResp strings.Builder
	fmt.Fprintf(&expectedResp, "Oneshot '%s' added", indexName)
	if strings.Compare(addOneshotResp, expectedResp.String()) == 0 {
		logf.Log.Error(err, "Failed response to add oneshot to splunk", "pod", podName, "addOneshotResp", addOneshotResp)
		return err
	}
	logf.Log.Info("File Ingested via add oneshot Successfully", "logFile", logFile, "addOneshotResp", addOneshotResp)
	return nil
}

// CopyFileToPod copies a file locally from srcPath to the destPath on the pod specified in podName
func CopyFileToPod(podName string, srcPath string, destPath string, deployment *Deployment) (string, string, error) {
	// Create tar file stream
	reader, writer := io.Pipe()
	if destPath != "/" && strings.HasSuffix(string(destPath[len(destPath)-1]), "/") {
		destPath = destPath[:len(destPath)-1]
	}
	go func() {
		defer writer.Close()
		err := cpMakeTar(srcPath, destPath, writer)
		if err != nil {
			return
		}
	}()
	var cmdArr []string

	cmdArr = []string{"tar", "-xf", "-"}
	destDir := path.Dir(destPath)
	if len(destDir) > 0 {
		cmdArr = append(cmdArr, "-C", destDir)
	}

	// Setup exec  command for pod
	pod := &corev1.Pod{}
	deployment.GetInstance(podName, pod)
	gvk, _ := apiutil.GVKForObject(pod, scheme.Scheme)
	restConfig, err := config.GetConfig()
	if err != nil {
		return "", "", err
	}
	restClient, err := apiutil.RESTClientForGVK(gvk, restConfig, serializer.NewCodecFactory(scheme.Scheme))
	if err != nil {
		return "", "", err
	}

	execReq := restClient.Post().Resource("pods").Name(podName).Namespace(deployment.testenv.namespace).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command: cmdArr,
		Stdin:   true,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}

	execReq.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, http.MethodPost, execReq.URL())
	if err != nil {
		return "", "", err
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  reader,
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return "", "", err
	}
	return stdout.String(), stderr.String(), nil
}

//go:linkname cpMakeTar k8s.io/kubernetes/pkg/kubectl/cmd/cp.makeTar
func cpMakeTar(srcPath, destPath string, writer io.Writer) error

// IngestFileViaMonitor ingests a file into an instance using the oneshot CLI
func IngestFileViaMonitor(logFile string, indexName string, podName string, deployment *Deployment) error {

	// Monitor log into specified index
	var addMonitorCmd strings.Builder
	splunkBin := "/opt/splunk/bin/splunk"
	username := "admin"
	password := "$(cat /mnt/splunk-secrets/password)"
	splunkCmd := "add monitor"

	fmt.Fprintf(&addMonitorCmd, "%s %s %s -index %s -auth %s:%s", splunkBin, splunkCmd, logFile, indexName, username, password)
	command := []string{"/bin/bash"}
	stdin := addMonitorCmd.String()
	addMonitorResp, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "stdin", stdin, "addMonitorResp", addMonitorResp, "stderr", stderr)
		return err
	}

	// Validate the expected CLI response
	var expectedResp strings.Builder
	fmt.Fprintf(&expectedResp, "Added monitor of '%s'", logFile)
	if strings.Compare(addMonitorResp, expectedResp.String()) == 0 {
		logf.Log.Error(err, "Failed response to add monitor to splunk", "pod", podName, "addMonitorResp", addMonitorResp)
		return err
	}
	logf.Log.Info("File Ingested via add monitor Successfully", "logFile", logFile, "addMonitorResp", addMonitorResp)
	return nil
}
