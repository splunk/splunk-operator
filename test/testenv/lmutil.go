package testenv

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type licenserLocalSlaveResponse struct {
	Entry []struct {
		Name    string `json:"name"`
		ID      string `json:"id"`
		Content struct {
			GUID                     []string `json:"guid"`
			LastTrackerdbServiceTime int      `json:"last_trackerdb_service_time"`
			LicenseKeys              []string `json:"license_keys"`
			MasterGUID               string   `json:"master_guid"`
			MasterURI                string   `json:"master_uri"`
		} `json:"content"`
	} `json:"entry"`
}

// CheckLicenseMasterConfigured checks if lm is configured on given pod
func CheckLicenseMasterConfigured(deployment *Deployment, podName string) bool {
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/licenser/localslave?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := licenserLocalSlaveResponse{}
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse health status")
		return false
	}
	licenseMaster := restResponse.Entry[0].Content.MasterURI
	logf.Log.Info("License Master configuration on POD", "POD", podName, "License Master", licenseMaster)
	return strings.Contains(licenseMaster, "license-master-service:8089")
}

// DownloadFromS3Bucket downloads license file from S3
func DownloadFromS3Bucket() (string, error) {
	dataBucket := "splk-test-data-bucket"
	location := "/test_licenses"
	item := "enterprise.lic"
	file, err := os.Create(item)
	if err != nil {
		logf.Log.Error(err, "Failed to create license file")
	}
	defer file.Close()

	sess, _ := session.NewSession(&aws.Config{Region: aws.String("us-west-2")})
	downloader := s3manager.NewDownloader(sess)
	numBytes, err := downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(dataBucket),
			Key:    aws.String(location + "/" + "enterprise.lic"),
		})
	if err != nil {
		logf.Log.Error(err, "Failed to download license file")
	}

	logf.Log.Info("Downloaded", "filename", file.Name(), "bytes", numBytes)
	return file.Name(), err
}
