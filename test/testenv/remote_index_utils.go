package testenv

import (
	"context"
	"encoding/json"
	"os"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

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

// RollHotToWarm rolls hot buckets to warm for a given index and pod
func RollHotToWarm(ctx context.Context, deployment *Deployment, podName string, indexName string) bool {
	stdin := "/opt/splunk/bin/splunk _internal call /data/indexes/" + indexName + "/roll-hot-buckets -auth admin:$(cat /mnt/splunk-secrets/password)"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	return true
}

// GenerateQueueVolumeSpec return SQSVolumeSpec struct with given values
func GenerateQueueVolumeSpec(volumeName string, secretRef string) enterpriseApi.SQSVolumeSpec {
	return enterpriseApi.SQSVolumeSpec{
		Name:      volumeName,
		SecretRef: secretRef,
	}
}

// GenerateIndexVolumeSpec return VolumeSpec struct with given values
func GenerateIndexVolumeSpec(volumeName string, endpoint string, secretRef string, provider string, storageType string, region string) enterpriseApi.VolumeSpec {
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

// GenerateIndexVolumeSpecAzure return VolumeSpec struct with given values for Azure
func GenerateIndexVolumeSpecAzure(volumeName string, endpoint string, secretRef string, provider string, storageType string) enterpriseApi.VolumeSpec {
	return enterpriseApi.VolumeSpec{
		Name:      volumeName,
		Endpoint:  endpoint,
		Path:      azureIndexesContainer,
		SecretRef: secretRef,
		Provider:  provider,
		Type:      storageType,
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

// GenerateVolumeSpecForProvider returns a VolumeSpec slice appropriate for the
// current ClusterProvider (eks, azure, gcp). For Azure it respects the
// AZURE_MANAGED_ID_ENABLED environment variable.
func (testenvInstance *TestCaseEnv) GenerateVolumeSpecForProvider(ctx context.Context, volumeName string) []enterpriseApi.VolumeSpec {
	secretName := testenvInstance.GetIndexSecretName()
	switch ClusterProvider {
	case "eks":
		return []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpec(volumeName, GetS3Endpoint(), secretName, "aws", "s3", GetDefaultS3Region())}
	case "azure":
		if os.Getenv("AZURE_MANAGED_ID_ENABLED") == "false" {
			return []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpecAzure(volumeName, GetAzureEndpoint(ctx), secretName, "azure", "blob")}
		}
		return []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpecAzureManagedID(volumeName, GetAzureEndpoint(ctx), "azure", "blob")}
	case "gcp":
		return []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpec(volumeName, GetGCPEndpoint(), secretName, "gcp", "gcs", GetDefaultS3Region())}
	default:
		testenvInstance.Log.Info("Failed to identify provider: Should be 'eks' or 'azure' or 'gcp'")
		return nil
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
