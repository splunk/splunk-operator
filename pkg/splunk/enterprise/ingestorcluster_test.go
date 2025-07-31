/*
Copyright 2025.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package enterprise

import (
	"path/filepath"
	"testing"
)

func init() {
	GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + readinessScriptLocation)
		return fileLocation
	}
	GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + livenessScriptLocation)
		return fileLocation
	}
	GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + startupScriptLocation)
		return fileLocation
	}
}

func TestApplyIngestorCluster(t *testing.T) {
	// funcCalls := []spltest.MockFuncCall{
	// 	{MetaName: "*v1.Secret-test-splunk-test-secret"},                  // 0
	// 	{MetaName: "*v1.Secret-test-splunk-test-secret"},                  // 1
	// 	{MetaName: "*v1.Secret-test-splunk-test-secret"},                  // 2
	// 	{MetaName: "*v1.ConfigMap-test-splunk-ingestor-stack1-configmap"}, // 3
	// 	{MetaName: "*v1.Service-test-splunk-stack1-ingestor-headless"},    // 4
	// 	{MetaName: "*v1.Service-test-splunk-stack1-ingestor-service"},     // 5
	// 	{MetaName: "*v1.StatefulSet-test-splunk-stack1-ingestor"},         // 6
	// 	{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},      // 7
	// 	{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},      // 8
	// 	{MetaName: "*v1.Secret-test-splunk-test-secret"},                  // 9
	// 	{MetaName: "*v1.Secret-test-splunk-stack1-ingestor-secret-v1"},    // 10
	// 	{MetaName: "*v1.StatefulSet-test-splunk-stack1-ingestor"},         // 11
	// 	{MetaName: "*v1.StatefulSet-test-splunk-stack1-ingestor"},         // 12
	// }
	// updateFuncCalls := []spltest.MockFuncCall{
	// 	{MetaName: "*v1.Secret-test-splunk-test-secret"},                  // 0
	// 	{MetaName: "*v1.Secret-test-splunk-test-secret"},                  // 1
	// 	{MetaName: "*v1.ConfigMap-test-splunk-ingestor-stack1-configmap"}, // 2
	// 	{MetaName: "*v1.Service-test-splunk-stack1-ingestor-headless"},    // 3
	// 	{MetaName: "*v1.Service-test-splunk-stack1-ingestor-service"},     // 4
	// 	{MetaName: "*v1.StatefulSet-test-splunk-stack1-ingestor"},         // 5
	// 	{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},      // 6
	// 	{MetaName: "*v1.Secret-test-splunk-test-secret"},                  // 7
	// 	{MetaName: "*v1.Secret-test-splunk-stack1-ingestor-secret-v1"},    // 8
	// 	{MetaName: "*v1.StatefulSet-test-splunk-stack1-ingestor"},         // 9
	// 	{MetaName: "*v1.StatefulSet-test-splunk-stack1-ingestor"},         // 10
	// 	{MetaName: "*v1.StatefulSet-test-splunk-stack1-ingestor"},         // 11
	// }

	// labels := map[string]string{
	// 	"app.kubernetes.io/component":  "versionedSecrets",
	// 	"app.kubernetes.io/managed-by": "splunk-operator",
	// }
	// listOpts := []client.ListOption{
	// 	client.InNamespace("test"),
	// 	client.MatchingLabels(labels),
	// }
	// listmockCall := []spltest.MockFuncCall{
	// 	{ListOpts: listOpts}}
	// createCalls := map[string][]spltest.MockFuncCall{
	// 	"Get": funcCalls,
	// 	"Create": {
	// 		funcCalls[0],  // *v1.Secret-test-splunk-test-secret
	// 		funcCalls[3],  // *v1.ConfigMap-test-splunk-ingestor-stack1-configmap
	// 		funcCalls[4],  // *v1.Service-test-splunk-stack1-ingestor-headless
	// 		funcCalls[5],  // *v1.Service-test-splunk-stack1-ingestor-service
	// 		funcCalls[7],  // *v1.ConfigMap-test-splunk-test-probe-configmap
	// 		funcCalls[10], // *v1.Secret-test-splunk-stack1-ingestor-secret-v1
	// 		funcCalls[6],  // *v1.StatefulSet-test-splunk-stack1-ingestor
	// 	},
	// 	"Update": {funcCalls[0]}, // Now expect StatefulSet update
	// 	"List":   {listmockCall[0]},
	// }
	// updateCalls := map[string][]spltest.MockFuncCall{
	// 	"Get": updateFuncCalls,
	// 	"Update": {
	// 		funcCalls[6], // Now expect StatefulSet update
	// 	},
	// 	"List": {listmockCall[0]},
	// }
	// current := enterpriseApi.IngestorCluster{
	// 	TypeMeta: metav1.TypeMeta{
	// 		Kind: "IngestorCluster",
	// 	},
	// 	ObjectMeta: metav1.ObjectMeta{
	// 		Name:      "stack1",
	// 		Namespace: "test",
	// 	},
	// }
	// revised := current.DeepCopy()
	// revised.Spec.Image = "splunk/test"
	// reconcile := func(c *spltest.MockClient, cr interface{}) error {
	// 	_, err := ApplyIngestorCluster(context.Background(), c, cr.(*enterpriseApi.IngestorCluster))
	// 	return err
	// }
	// spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyIngestorCluster", &current, revised, createCalls, updateCalls, reconcile, true)

	// currentTime := metav1.NewTime(time.Now())
	// revised.ObjectMeta.DeletionTimestamp = &currentTime
	// revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	// deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
	// 	_, err := ApplyIngestorCluster(context.Background(), c, cr.(*enterpriseApi.IngestorCluster))
	// 	return true, err
	// }
	// splunkDeletionTester(t, revised, deleteFunc)

	// current.Spec.CommonSplunkSpec.LivenessInitialDelaySeconds = -1
	// c := spltest.NewMockClient()
	// ctx := context.TODO()
	// _ = errors.New(splcommon.Rerr)
	// _, err := ApplyIngestorCluster(ctx, c, &current)
	// if err == nil {
	// 	t.Errorf("Expected error")
	// }
}
