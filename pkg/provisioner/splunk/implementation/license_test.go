package impl

import (
	"context"
	"testing"

	//splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	//licensemodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license"
	//provisioner "github.com/splunk/splunk-operator/pkg/provisioner/splunk"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetGetLicense(t *testing.T) {
	/*callGetClusterManagerHealth = func(ctx context.Context, p *splunkProvisioner) (*[]licensemodel.License, error) {
		healthData := []licensemodel.ClusterManagerHealthContent{}
		return &healthData, nil
	}*/
	provisioner := setCreds(t)
	conditions := &[]metav1.Condition{}

	ctx := context.TODO()

	_, err := provisioner.GetLicense(ctx, conditions)
	if err != nil {
		t.Errorf("fixture: error in set cluster manager %v", err)
	}
	if conditions == nil || len(*conditions) == 0 {
		t.Errorf("fixture: error in conditions for lm %v", err)
	}
}

func TestGetLicenseLocalPeer(t *testing.T) {
	/*callGetClusterManagerHealth = func(ctx context.Context, p *splunkProvisioner) (*[]licensemodel.LicenseLocalPeer, error) {
		healthData := []licensemodel.ClusterManagerHealthContent{}
		return &healthData, nil
	}*/
	provisioner := setCreds(t)
	conditions := &[]metav1.Condition{}

	ctx := context.TODO()

	_, err := provisioner.GetLicenseLocalPeer(ctx, conditions)
	if err != nil {
		t.Errorf("fixture: error in set cluster manager %v", err)
	}
	if conditions == nil || len(*conditions) == 0 {
		t.Errorf("fixture: error in conditions for license manager %v", err)
	}
}
