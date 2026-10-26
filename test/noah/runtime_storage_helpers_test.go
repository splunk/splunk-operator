// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package noah

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

type krakenRuntimeExpectations struct {
	serviceAccount      string
	authRegion          string
	objectStorePath     string
	objectStoreEndpoint string
	queueURL            string
	queueEndpoint       string
}

type indexerDefaults struct {
	Splunk struct {
		Conf struct {
			Indexes struct {
				Content map[string]map[string]string `json:"content"`
			} `json:"indexes"`
		} `json:"conf"`
	} `json:"splunk"`
}

// checkKrakenExternalStorage is the common precondition for every Kraken Noah
// scenario. It rejects a mixed MinIO/AWS environment before a scenario can
// report success against the wrong storage backend, and returns the Kraken
// runtime expectations the IndexerCluster is wired to.
func checkKrakenExternalStorage(ctx context.Context) (krakenRuntimeExpectations, error) {
	expected, err := loadKrakenRuntimeExpectations()
	if err != nil {
		return krakenRuntimeExpectations{}, err
	}
	if err := validateKrakenRuntimeExpectations(expected); err != nil {
		return krakenRuntimeExpectations{}, err
	}

	idxc := &enterpriseApi.IndexerCluster{}
	if err := testcaseEnvInstance.GetKubeClient().Get(
		ctx,
		types.NamespacedName{Namespace: operatorNamespace, Name: clusterName},
		idxc,
	); err != nil {
		return krakenRuntimeExpectations{}, fmt.Errorf("get IndexerCluster %s/%s: %w", operatorNamespace, clusterName, err)
	}
	if idxc.Spec.ServiceAccount != expected.serviceAccount {
		return krakenRuntimeExpectations{}, fmt.Errorf(
			"IndexerCluster %s/%s uses ServiceAccount %q, want %q",
			operatorNamespace, clusterName, idxc.Spec.ServiceAccount, expected.serviceAccount,
		)
	}
	if err := validateIndexerDefaults(idxc.Spec.Defaults, expected); err != nil {
		return krakenRuntimeExpectations{}, err
	}

	minio, err := findMinIOResources(ctx)
	if err != nil {
		return krakenRuntimeExpectations{}, err
	}
	if len(minio) > 0 {
		return krakenRuntimeExpectations{}, fmt.Errorf(
			"Kraken Noah tests require external S3/SQS; found MinIO resources: %s", strings.Join(minio, ", "),
		)
	}

	return expected, nil
}

func validateKrakenRuntimeExpectations(expected krakenRuntimeExpectations) error {
	if strings.TrimSpace(expected.serviceAccount) == "" {
		return fmt.Errorf("Kraken workload-identity ServiceAccount must not be empty")
	}
	if strings.TrimSpace(expected.authRegion) == "" {
		return fmt.Errorf("Kraken AWS region must not be empty")
	}
	if err := validateRuntimeURL("object-store path", expected.objectStorePath, "s3"); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"object-store endpoint": expected.objectStoreEndpoint,
		"queue endpoint":        expected.queueEndpoint,
		"queue URL":             expected.queueURL,
	} {
		if err := validateRuntimeURL(field, value, "https"); err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(value), "minio") {
			return fmt.Errorf("%s must not reference MinIO in Kraken tests", field)
		}
	}
	return nil
}

func validateRuntimeURL(field, value, requiredScheme string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse Kraken %s: %w", field, err)
	}
	if parsed.Scheme != requiredScheme || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("Kraken %s must be a %s URL with a host", field, requiredScheme)
	}
	return nil
}

// findMinIOResources lists the Noah release's MinIO-labelled objects as
// kind/name, sorted.
func findMinIOResources(ctx context.Context) ([]string, error) {
	kubeClient := testcaseEnvInstance.GetKubeClient()
	listOptions := []client.ListOption{
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{"app.kubernetes.io/instance": noahDeployment},
	}
	lists := []struct {
		kind string
		list client.ObjectList
	}{
		{"Deployment", &appsv1.DeploymentList{}},
		{"StatefulSet", &appsv1.StatefulSetList{}},
		{"Service", &corev1.ServiceList{}},
		{"Secret", &corev1.SecretList{}},
		{"Job", &batchv1.JobList{}},
		{"Pod", &corev1.PodList{}},
	}

	resources := make([]string, 0)
	for _, entry := range lists {
		if err := kubeClient.List(ctx, entry.list, listOptions...); err != nil {
			return nil, fmt.Errorf("list %s objects in %s: %w", entry.kind, operatorNamespace, err)
		}
		items, err := meta.ExtractList(entry.list)
		if err != nil {
			return nil, fmt.Errorf("read %s list: %w", entry.kind, err)
		}
		for _, item := range items {
			object, ok := item.(client.Object)
			if !ok {
				return nil, fmt.Errorf("unexpected %s list item %T", entry.kind, item)
			}
			if strings.HasPrefix(object.GetLabels()["app.kubernetes.io/component"], "minio") {
				resources = append(resources, entry.kind+"/"+object.GetName())
			}
		}
	}

	sort.Strings(resources)
	return resources, nil
}

// loadKrakenRuntimeExpectations reports every missing variable at once.
func loadKrakenRuntimeExpectations() (krakenRuntimeExpectations, error) {
	var errs []error
	env := func(name string) string {
		value, err := requiredTestEnv(name)
		if err != nil {
			errs = append(errs, err)
		}
		return value
	}

	expected := krakenRuntimeExpectations{
		serviceAccount:      env("NOAH_TEST_SPLUNK_SERVICE_ACCOUNT"),
		authRegion:          env("NOAH_TEST_SMARTSTORE_AUTH_REGION"),
		objectStorePath:     env("NOAH_TEST_SMARTSTORE_OBJECT_STORE_PATH"),
		objectStoreEndpoint: env("NOAH_TEST_SMARTSTORE_OBJECT_STORE_ENDPOINT"),
		queueEndpoint:       env("NOAH_TEST_SMARTSTORE_QUEUE_ENDPOINT"),
		queueURL:            env("NOAH_TEST_SMARTSTORE_QUEUE_URL"),
	}
	if err := errors.Join(errs...); err != nil {
		return krakenRuntimeExpectations{}, err
	}
	return expected, nil
}

func requiredTestEnv(name string) (string, error) {
	value, found := os.LookupEnv(name)
	if !found {
		return "", fmt.Errorf("%s must be set", name)
	}
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	return value, nil
}

func envByName(env []corev1.EnvVar) map[string]string {
	result := make(map[string]string, len(env))
	for _, variable := range env {
		result[variable.Name] = variable.Value
	}
	return result
}

func containerByName(containers []corev1.Container, name string) (corev1.Container, bool) {
	for _, container := range containers {
		if container.Name == name {
			return container, true
		}
	}
	return corev1.Container{}, false
}

func validateIndexerDefaults(rawDefaults string, expected krakenRuntimeExpectations) error {
	defaults := indexerDefaults{}
	if err := yaml.Unmarshal([]byte(rawDefaults), &defaults); err != nil {
		return fmt.Errorf("parse IndexerCluster defaults: %w", err)
	}

	stanzas := []struct {
		name     string
		settings [][2]string
	}{
		{"volume:remote_store", [][2]string{
			{"storageType", "remote"},
			{"path", expected.objectStorePath},
			{"remote.s3.endpoint", expected.objectStoreEndpoint},
			{"remote.s3.signature_version", "v4"},
			{"remote.s3.auth_region", expected.authRegion},
			{"remote.s3.sqs.endpoint", expected.queueEndpoint},
			{"remote.s3.sqs.queue_url", expected.queueURL},
			{"remote.s3.supports_versioning", "false"},
		}},
		{"main", [][2]string{
			{"remotePath", "volume:remote_store/main"},
		}},
	}
	for _, stanza := range stanzas {
		content, found := defaults.Splunk.Conf.Indexes.Content[stanza.name]
		if !found {
			return fmt.Errorf("IndexerCluster defaults have no %s stanza", stanza.name)
		}
		for _, setting := range stanza.settings {
			key, want := setting[0], setting[1]
			got, found := content[key]
			if !found {
				return fmt.Errorf("IndexerCluster defaults [%s] has no %s", stanza.name, key)
			}
			if got != want {
				return fmt.Errorf("IndexerCluster defaults [%s] %s = %q, want %q", stanza.name, key, got, want)
			}
		}
	}
	return nil
}

func parseBtoolSettings(output string) map[string]string {
	settings := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, " = ", 2)
		if len(parts) != 2 {
			continue
		}
		leftFields := strings.Fields(parts[0])
		if len(leftFields) == 0 {
			continue
		}
		settings[leftFields[len(leftFields)-1]] = strings.TrimSpace(parts[1])
	}
	return settings
}

func effectiveIndexesSettings(ctx context.Context, podName, stanza string) (map[string]string, error) {
	stdout, stderr, err := deployment.PodExecCommand(
		ctx,
		podName,
		[]string{"/opt/splunk/bin/splunk", "btool", "indexes", "list", stanza, "--debug"},
		"",
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("btool failed for stanza %s on %s: %w: %s", stanza, podName, err, stderr)
	}

	settings := parseBtoolSettings(stdout)
	if len(settings) == 0 {
		return nil, fmt.Errorf("btool returned no settings for stanza %s on %s", stanza, podName)
	}
	return settings, nil
}
