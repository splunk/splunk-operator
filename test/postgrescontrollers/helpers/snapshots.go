// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
package helpers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	snapshotclient "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// RequireVolumeSnapshotClass verifies the cluster-scoped snapshot contract
// required by PostgreSQL snapshot workflows.
func RequireVolumeSnapshotClass(ctx context.Context, name string) (snapshotclient.Interface, string) {
	GinkgoHelper()
	restConfig, err := config.GetConfig()
	Expect(err).To(Succeed())
	snapshots, err := snapshotclient.NewForConfig(restConfig)
	Expect(err).To(Succeed())
	snapshotClass, err := snapshots.SnapshotV1().VolumeSnapshotClasses().Get(ctx, name, metav1.GetOptions{})
	Expect(err).To(Succeed(), "required VolumeSnapshotClass %q is unavailable", name)
	Expect(snapshotClass.Driver).NotTo(BeEmpty())
	Expect(string(snapshotClass.DeletionPolicy)).To(Equal("Delete"),
		"the E2E VolumeSnapshotClass must delete backing snapshots during cleanup")
	return snapshots, snapshotClass.Driver
}
