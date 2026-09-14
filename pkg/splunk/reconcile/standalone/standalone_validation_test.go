// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package standalone

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
)

func TestValidateSmartstoreSpec(t *testing.T) {
	valid := &enterpriseApi.SmartStoreSpec{
		VolList:   []enterpriseApi.VolumeSpec{{Name: "s3-volume", Endpoint: "https://s3.example.com", Path: "bucket"}},
		IndexList: []enterpriseApi.IndexSpec{{Name: "salesdata", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{VolName: "s3-volume"}}},
	}

	tests := []struct {
		name      string
		spec      *enterpriseApi.SmartStoreSpec
		wantError bool
	}{
		{name: "valid", spec: valid},
		{name: "empty", spec: nil},
		{name: "missing endpoint", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.VolList[0].Endpoint = ""
			return spec
		}(), wantError: true},
		{name: "missing volume name", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.VolList[0].Name = ""
			return spec
		}(), wantError: true},
		{name: "missing volume path", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.VolList[0].Path = ""
			return spec
		}(), wantError: true},
		{name: "indexes without volumes", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.VolList = nil
			return spec
		}(), wantError: true},
		{name: "duplicate volumes", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.VolList = append(spec.VolList, spec.VolList[0])
			return spec
		}(), wantError: true},
		{name: "duplicate indexes", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.IndexList = append(spec.IndexList, spec.IndexList[0])
			return spec
		}(), wantError: true},
		{name: "missing default volume", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.Defaults.VolName = "missing-volume"
			return spec
		}(), wantError: true},
		{name: "missing index volume", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.IndexList[0].VolName = "missing-volume"
			return spec
		}(), wantError: true},
		{name: "default volume applies to indexes", spec: func() *enterpriseApi.SmartStoreSpec {
			spec := valid.DeepCopy()
			spec.Defaults.VolName = "s3-volume"
			spec.IndexList[0].VolName = ""
			return spec
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSmartstoreSpec(tt.spec)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
