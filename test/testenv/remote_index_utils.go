package testenv

import (
	"context"
	"encoding/json"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DataIndexesResponse struct for /data/indexes response
type DataIndexesResponse struct {
	Entry []IndexEntry `json:"entry"`
}

// IndexEntry struct of index data response returned by /data/indexes endpoint
type IndexEntry struct {
	Name    string `json:"name"`
	Content struct {
		MaxGlobalDataSizeMB    int `json:"maxGlobalDataSizeMB"`
		MaxGlobalRawDataSizeMB int `json:"maxGlobalRawDataSizeMB"`
	}
}

// GetServiceDataIndexes returns output of services data indexes
func GetServiceDataIndexes(ctx context.Context, deployment *Deployment, podName string) (DataIndexesResponse, error) {
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/data/indexes?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	restResponse := DataIndexesResponse{}
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return restResponse, err
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse data/indexes response")
	}
	return restResponse, err
}

// GetIndexOnPod get list of indexes on given pod
func GetIndexOnPod(ctx context.Context, deployment *Deployment, podName string, indexName string) (bool, IndexEntry) {
	restResponse, err := GetServiceDataIndexes(ctx, deployment, podName)
	indexData := IndexEntry{}
	if err != nil {
		logf.Log.Error(err, "Failed to parse data/indexes response")
		return false, indexData
	}
	indexFound := false
	for _, entry := range restResponse.Entry {
		if entry.Name == indexName {
			indexFound = true
			indexData = entry
			break
		}
	}
	return indexFound, indexData
}

// RestartSplunk Restart splunk inside the container
func RestartSplunk(ctx context.Context, deployment *Deployment, podName string) bool {
	stdin := "/opt/splunk/bin/splunk restart -auth admin:$(cat /mnt/splunk-secrets/password)"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	return true
}

// RollHotToWarm rolls hot buckets to warm for a given index and pod
func RollHotToWarm(ctx context.Context, deployment *Deployment, podName string, indexName string) bool {
	stdin := "/opt/splunk/bin/splunk _internal call /data/indexes/" + indexName + "/roll-hot-buckets admin:$(cat /mnt/splunk-secrets/password)"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	return true
}

// GenerateIndexVolumeSpec return VolumeSpec struct with given values
func GenerateIndexVolumeSpec(volumeName string, endpoint string, secretRef string, provider string, storageType string) enterpriseApi.VolumeSpec {
	return enterpriseApi.VolumeSpec{
		Name:      volumeName,
		Endpoint:  endpoint,
		Path:      testIndexesS3Bucket,
		SecretRef: secretRef,
		Provider:  provider,
		Type:      storageType,
		Region:    region,
	}
}

// GenerateIndexVolumeSpecAzureManagedID return VolumeSpec struct with given values for Azure using Managed Identities
func GenerateIndexVolumeSpecAzureManagedID(volumeName string, endpoint string, provider string, storageType string) enterpriseApi.VolumeSpec {
	return enterpriseApi.VolumeSpec{
		Name:     volumeName,
		Endpoint: endpoint,
		Path:     azureIndexesContainer,
		Provider: provider,
		Type:     storageType,
	}
}

// GenerateIndexSpec return VolumeSpec struct with given values
func GenerateIndexSpec(indexName string, volName string) enterpriseApi.IndexSpec {
	return enterpriseApi.IndexSpec{
		Name:       indexName,
		RemotePath: indexName,
		IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
			VolName: volName,
		},
	}
}
