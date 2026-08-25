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
package postgrescontrollers

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	pgtesthelpers "github.com/splunk/splunk-operator/test/postgrescontrollers/helpers"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clusterMetricFamilyA  = "cnpg_splunk_operator_cluster_platform_probe_observed_value"
	clusterMetricFamilyB  = "cnpg_splunk_operator_cluster_secondary_probe_observed_value"
	databaseMetricFamily  = "cnpg_splunk_operator_database_appdb_application_rows_row_count"
	defaultCNPGMetricName = "cnpg_collector_up"
)

var _ = Describe("postgrescontrollers, integration, postgres-metrics",
	Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {
		var testcaseEnvInst *testenv.TestCaseEnv
		var deployment *testenv.Deployment
		var dumpFailure func(SpecContext)

		BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
			dumpFailure = nil
		})

		AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
			if CurrentSpecReport().Failed() && dumpFailure != nil {
				dumpFailure(ctx)
			}
			Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("exposes and reconciles cluster and database custom metrics",
			Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
			NodeTimeout(testenv.MediumTimeout),
			func(ctx SpecContext) {
				namespace := testcaseEnvInst.GetName()
				kubeClient := testcaseEnvInst.GetKubeClient()
				metricsClient, err := pgtesthelpers.NewMetricsClient()
				Expect(err).To(Succeed())

				clusterSourceA := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-metrics-a", Namespace: namespace},
					Data:       map[string]string{cnpginfra.MonitoringCMKey: clusterMetricA(7, "initial")},
				}
				clusterSourceB := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-metrics-b", Namespace: namespace},
					Data:       map[string]string{cnpginfra.MonitoringCMKey: clusterMetricB()},
				}
				Expect(kubeClient.Create(ctx, clusterSourceA)).To(Succeed())
				Expect(kubeClient.Create(ctx, clusterSourceB)).To(Succeed())

				pgClass := createPGClass(ctx, kubeClient, namespace)
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "metrics-cluster", Namespace: namespace},
					Spec: platformv1alpha1.PostgresClusterSpec{
						Class:                 pgClass.Name,
						ClusterDeletionPolicy: ptr.To("Delete"),
						Monitoring: &platformv1alpha1.PostgresClusterMonitoring{
							PostgreSQLMetrics: ptr.To(true),
							CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{
								{LocalObjectReference: corev1.LocalObjectReference{Name: clusterSourceA.Name}, Key: cnpginfra.MonitoringCMKey},
								{LocalObjectReference: corev1.LocalObjectReference{Name: clusterSourceB.Name}, Key: cnpginfra.MonitoringCMKey},
							},
						},
					},
				}
				Expect(kubeClient.Create(ctx, cluster)).To(Succeed())
				clusterKey := types.NamespacedName{Name: cluster.Name, Namespace: namespace}
				pgtesthelpers.WaitForReadyPostgresCluster(ctx, kubeClient, clusterKey)

				database := &platformv1alpha1.PostgresDatabase{
					ObjectMeta: metav1.ObjectMeta{Name: "metrics-databases", Namespace: namespace},
					Spec: platformv1alpha1.PostgresDatabaseSpec{
						ClusterRef: corev1.LocalObjectReference{Name: cluster.Name},
						Databases: []platformv1alpha1.DatabaseDefinition{
							{Name: "appdb", DeletionPolicy: "Delete"},
							{Name: "otherdb", DeletionPolicy: "Delete"},
						},
					},
				}
				Expect(kubeClient.Create(ctx, database)).To(Succeed())
				databaseKey := types.NamespacedName{Name: database.Name, Namespace: namespace}
				dumpFailure = pgtesthelpers.CustomMetricsFailureDump(kubeClient, metricsClient, clusterKey, databaseKey)

				By("waiting for both logical databases before attaching the application query")
				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresDatabase{}
					g.Expect(kubeClient.Get(ctx, databaseKey, current)).To(Succeed())
					if current.Status.Phase != nil && *current.Status.Phase == "Failed" {
						StopTrying(pgtesthelpers.PostgresDatabaseFailure(current)).Now()
					}
					g.Expect(current.Status.Phase).To(HaveValue(Equal("Ready")))
					g.Expect(current.Status.Databases).To(HaveLen(2))
					for _, name := range []string{"appdb", "otherdb"} {
						info, found := databaseInfo(current, name)
						g.Expect(found).To(BeTrue(), "database %q was not published in status", name)
						g.Expect(info.Ready).To(BeTrue())
					}
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				By("creating distinguishable fixtures and granting the exporter read access")
				fixtures := []struct {
					databaseName string
					rowCount     int
				}{{databaseName: "appdb", rowCount: 3}, {databaseName: "otherdb", rowCount: 9}}
				for _, fixture := range fixtures {
					_, err = pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, kubeClient, deployment, clusterKey, fixture.databaseName, fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS custom_metrics_fixture (id integer PRIMARY KEY);
TRUNCATE custom_metrics_fixture;
INSERT INTO custom_metrics_fixture SELECT generate_series(1, %d);
GRANT CONNECT ON DATABASE %s TO cnpg_metrics_exporter;
GRANT USAGE ON SCHEMA public TO cnpg_metrics_exporter;
GRANT SELECT ON TABLE public.custom_metrics_fixture TO cnpg_metrics_exporter;
`, fixture.rowCount, fixture.databaseName))
					Expect(err).To(Succeed())
				}

				databaseSource := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "database-metrics", Namespace: namespace},
					Data:       map[string]string{cnpginfra.MonitoringCMKey: databaseMetric()},
				}
				Expect(kubeClient.Create(ctx, databaseSource)).To(Succeed())

				By("publishing the appdb custom-metrics contribution")
				Eventually(func() error {
					current := &platformv1alpha1.PostgresDatabase{}
					if err := kubeClient.Get(ctx, databaseKey, current); err != nil {
						return err
					}
					found := false
					for i := range current.Spec.Databases {
						if current.Spec.Databases[i].Name == "appdb" {
							found = true
							current.Spec.Databases[i].Monitoring = &platformv1alpha1.DatabaseMonitoring{
								CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
									LocalObjectReference: corev1.LocalObjectReference{Name: databaseSource.Name},
									Key:                  cnpginfra.MonitoringCMKey,
								}},
							}
						}
					}
					if !found {
						return fmt.Errorf("PostgresDatabase %s has no appdb definition", databaseKey)
					}
					return kubeClient.Update(ctx, current)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				generatedKey := types.NamespacedName{Name: cluster.Name + "-metrics", Namespace: namespace}
				var confirmedRevision, confirmedData, confirmedHash string
				By("proving provider configuration and the database-to-cluster acknowledgement")
				Eventually(func(g Gomega) {
					currentCluster := &platformv1alpha1.PostgresCluster{}
					g.Expect(kubeClient.Get(ctx, clusterKey, currentCluster)).To(Succeed())
					pgtesthelpers.StopIfPostgresClusterFailed(currentCluster)
					clusterCondition := meta.FindStatusCondition(currentCluster.Status.Conditions, "CustomMetricsReady")
					g.Expect(clusterCondition).NotTo(BeNil())
					if clusterCondition == nil {
						return
					}
					g.Expect(clusterCondition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(clusterCondition.Reason).To(Equal("CustomMetricsReady"))
					g.Expect(clusterCondition.ObservedGeneration).To(Equal(currentCluster.Generation))

					currentDatabase := &platformv1alpha1.PostgresDatabase{}
					g.Expect(kubeClient.Get(ctx, databaseKey, currentDatabase)).To(Succeed())
					if currentDatabase.Status.Phase != nil && *currentDatabase.Status.Phase == "Failed" {
						StopTrying(pgtesthelpers.PostgresDatabaseFailure(currentDatabase)).Now()
					}
					g.Expect(currentDatabase.Status.Phase).To(HaveValue(Equal("Ready")))
					databaseCondition := meta.FindStatusCondition(currentDatabase.Status.Conditions, "CustomMetricsReady")
					g.Expect(databaseCondition).NotTo(BeNil())
					if databaseCondition == nil {
						return
					}
					g.Expect(databaseCondition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(databaseCondition.Reason).To(Equal("CustomMetricsReady"))
					g.Expect(databaseCondition.ObservedGeneration).To(Equal(currentDatabase.Generation))
					g.Expect(currentDatabase.Status.ObservedGeneration).To(HaveValue(Equal(currentDatabase.Generation)))
					g.Expect(currentDatabase.Status.CustomMetricsPublication).NotTo(BeNil())
					if currentDatabase.Status.CustomMetricsPublication == nil {
						return
					}
					g.Expect(currentDatabase.Status.CustomMetricsPublication.ObservedGeneration).To(Equal(currentDatabase.Generation))
					contribution, found := databaseContribution(currentDatabase, "appdb")
					g.Expect(found).To(BeTrue())
					g.Expect(contribution.Exists).To(BeTrue())
					g.Expect(contribution.Revision).NotTo(BeEmpty())
					acknowledgement, found := databaseAcknowledgement(currentCluster, currentDatabase, "appdb")
					g.Expect(found).To(BeTrue())
					g.Expect(acknowledgement.DesiredRevision).To(Equal(contribution.Revision))
					g.Expect(acknowledgement.AppliedRevision).To(Equal(contribution.Revision))
					g.Expect(acknowledgement.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(acknowledgement.Reason).To(Equal("CustomMetricsReady"))

					cnpg := &cnpgv1.Cluster{}
					g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
					generated := &corev1.ConfigMap{}
					g.Expect(kubeClient.Get(ctx, generatedKey, generated)).To(Succeed())
					owner := metav1.GetControllerOf(generated)
					g.Expect(owner).NotTo(BeNil())
					if owner == nil {
						return
					}
					g.Expect(owner.UID).To(Equal(cnpg.UID))
					entries, err := pgtesthelpers.GeneratedMetricEntries(generated)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(entries).To(HaveLen(3))
					g.Expect(entries).To(HaveKey("splunk_operator_cluster:platform_probe"))
					g.Expect(entries).To(HaveKey("splunk_operator_cluster:secondary_probe"))
					databaseEntry, found := entries["splunk_operator_database:appdb:application_rows"]
					g.Expect(found).To(BeTrue())
					g.Expect(databaseEntry.TargetDatabases).To(Equal([]string{"appdb"}))
					g.Expect(cnpgMetricSelectorCount(cnpg, generated.Name)).To(Equal(1))
					g.Expect(cnpg.Status.ConfigMapResourceVersion.Metrics).To(HaveKeyWithValue(generated.Name, generated.ResourceVersion))

					confirmedRevision = contribution.Revision
					confirmedData = generated.Data[cnpginfra.MonitoringCMKey]
					confirmedHash = generated.Annotations[cnpginfra.MonitoringCMHashAnnotation]
					g.Expect(confirmedData).NotTo(BeEmpty())
					g.Expect(confirmedHash).NotTo(BeEmpty())
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				By("proving the generated queries are exposed by the live CNPG exporter")
				Eventually(func(g Gomega) {
					families, err := pgtesthelpers.ScrapePostgresMetrics(ctx, kubeClient, metricsClient, clusterKey)
					g.Expect(err).NotTo(HaveOccurred())
					expectActiveCustomMetricSet(g, families, "initial", 7)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				By("updating one source without replacing other owners")
				Eventually(func() error {
					current := &corev1.ConfigMap{}
					if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(clusterSourceA), current); err != nil {
						return err
					}
					current.Data[cnpginfra.MonitoringCMKey] = clusterMetricA(13, "updated")
					return kubeClient.Update(ctx, current)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				var updatedData, updatedHash, updatedResourceVersion string
				var updatedUID types.UID
				Eventually(func(g Gomega) {
					generated := &corev1.ConfigMap{}
					g.Expect(kubeClient.Get(ctx, generatedKey, generated)).To(Succeed())
					updatedData = generated.Data[cnpginfra.MonitoringCMKey]
					updatedHash = generated.Annotations[cnpginfra.MonitoringCMHashAnnotation]
					updatedUID = generated.UID
					updatedResourceVersion = generated.ResourceVersion
					g.Expect(updatedData).NotTo(Equal(confirmedData))
					g.Expect(updatedHash).NotTo(Equal(confirmedHash))
					g.Expect(updatedUID).NotTo(BeEmpty())
					g.Expect(updatedResourceVersion).NotTo(BeEmpty())
					cnpg := &cnpgv1.Cluster{}
					g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
					g.Expect(cnpg.Status.ConfigMapResourceVersion.Metrics).To(HaveKeyWithValue(generated.Name, generated.ResourceVersion))
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				Eventually(func(g Gomega) {
					families, err := pgtesthelpers.ScrapePostgresMetrics(ctx, kubeClient, metricsClient, clusterKey)
					g.Expect(err).NotTo(HaveOccurred())
					expectActiveCustomMetricSet(g, families, "updated", 13)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				By("preserving the last-known-good aggregate while one source is invalid")
				generatedWatch, err := metricsClient.CoreV1().ConfigMaps(namespace).Watch(ctx, metav1.ListOptions{
					FieldSelector:   fields.OneTermEqualSelector("metadata.name", generatedKey.Name).String(),
					ResourceVersion: updatedResourceVersion,
				})
				Expect(err).To(Succeed())
				defer generatedWatch.Stop()

				Eventually(func() error {
					current := &corev1.ConfigMap{}
					if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(clusterSourceA), current); err != nil {
						return err
					}
					current.Data[cnpginfra.MonitoringCMKey] = invalidClusterMetricA()
					return kubeClient.Update(ctx, current)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(kubeClient.Get(ctx, clusterKey, current)).To(Succeed())
					condition := meta.FindStatusCondition(current.Status.Conditions, "CustomMetricsReady")
					g.Expect(condition).NotTo(BeNil())
					if condition == nil {
						return
					}
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(condition.Reason).To(Equal("InvalidQueryDefinition"))
					g.Expect(condition.ObservedGeneration).To(Equal(current.Generation))
					g.Expect(condition.Message).To(ContainSubstring(clusterSourceA.Name))
					g.Expect(condition.Message).To(ContainSubstring("histogram"))
					generated := &corev1.ConfigMap{}
					g.Expect(kubeClient.Get(ctx, generatedKey, generated)).To(Succeed())
					g.Expect(generated.UID).To(Equal(updatedUID))
					g.Expect(generated.ResourceVersion).To(Equal(updatedResourceVersion))
					g.Expect(generated.Data[cnpginfra.MonitoringCMKey]).To(Equal(updatedData))
					g.Expect(generated.Annotations[cnpginfra.MonitoringCMHashAnnotation]).To(Equal(updatedHash))

					currentDatabase := &platformv1alpha1.PostgresDatabase{}
					g.Expect(kubeClient.Get(ctx, databaseKey, currentDatabase)).To(Succeed())
					if currentDatabase.Status.Phase != nil && *currentDatabase.Status.Phase == "Failed" {
						StopTrying(pgtesthelpers.PostgresDatabaseFailure(currentDatabase)).Now()
					}
					g.Expect(currentDatabase.Status.Phase).To(HaveValue(Equal("Ready")))
					acknowledgement, found := databaseAcknowledgement(current, currentDatabase, "appdb")
					g.Expect(found).To(BeTrue())
					g.Expect(acknowledgement.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(acknowledgement.AppliedRevision).To(Equal(confirmedRevision))
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				Eventually(func(g Gomega) {
					families, err := pgtesthelpers.ScrapePostgresMetrics(ctx, kubeClient, metricsClient, clusterKey)
					g.Expect(err).NotTo(HaveOccurred())
					expectActiveCustomMetricSet(g, families, "updated", 13)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				Consistently(func() error {
					select {
					case event, open := <-generatedWatch.ResultChan():
						if !open {
							return fmt.Errorf("generated ConfigMap watch closed before source recovery")
						}
						if event.Type == watch.Bookmark {
							return nil
						}
						objectMeta, err := meta.Accessor(event.Object)
						if err != nil {
							return fmt.Errorf("reading generated ConfigMap watch event: %w", err)
						}
						return fmt.Errorf(
							"generated ConfigMap changed during source invalidity: event=%s uid=%s resourceVersion=%s",
							event.Type, objectMeta.GetUID(), objectMeta.GetResourceVersion(),
						)
					default:
						return nil
					}
				}, testenv.ConsistentDuration, testenv.ConsistentPollInterval).Should(Succeed())

				By("recovering from the source update without an unrelated trigger")
				Eventually(func() error {
					current := &corev1.ConfigMap{}
					if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(clusterSourceA), current); err != nil {
						return err
					}
					current.Data[cnpginfra.MonitoringCMKey] = clusterMetricA(13, "updated")
					return kubeClient.Update(ctx, current)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(kubeClient.Get(ctx, clusterKey, current)).To(Succeed())
					condition := meta.FindStatusCondition(current.Status.Conditions, "CustomMetricsReady")
					g.Expect(condition).NotTo(BeNil())
					if condition == nil {
						return
					}
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.Reason).To(Equal("CustomMetricsReady"))
					g.Expect(condition.ObservedGeneration).To(Equal(current.Generation))
					generated := &corev1.ConfigMap{}
					g.Expect(kubeClient.Get(ctx, generatedKey, generated)).To(Succeed())
					g.Expect(generated.Data[cnpginfra.MonitoringCMKey]).To(Equal(updatedData))
					g.Expect(generated.Annotations[cnpginfra.MonitoringCMHashAnnotation]).To(Equal(updatedHash))
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				By("removing only the database participant")
				Eventually(func() error {
					current := &platformv1alpha1.PostgresDatabase{}
					if err := kubeClient.Get(ctx, databaseKey, current); err != nil {
						return err
					}
					found := false
					for i := range current.Spec.Databases {
						if current.Spec.Databases[i].Name == "appdb" {
							found = true
							current.Spec.Databases[i].Monitoring = nil
						}
					}
					if !found {
						return fmt.Errorf("PostgresDatabase %s has no appdb definition", databaseKey)
					}
					return kubeClient.Update(ctx, current)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				Eventually(func(g Gomega) {
					currentDatabase := &platformv1alpha1.PostgresDatabase{}
					g.Expect(kubeClient.Get(ctx, databaseKey, currentDatabase)).To(Succeed())
					if currentDatabase.Status.Phase != nil && *currentDatabase.Status.Phase == "Failed" {
						StopTrying(pgtesthelpers.PostgresDatabaseFailure(currentDatabase)).Now()
					}
					g.Expect(currentDatabase.Status.Phase).To(HaveValue(Equal("Ready")))
					condition := meta.FindStatusCondition(currentDatabase.Status.Conditions, "CustomMetricsReady")
					g.Expect(condition).NotTo(BeNil())
					if condition == nil {
						return
					}
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.Reason).To(Equal("CustomMetricsDisabled"))
					g.Expect(condition.ObservedGeneration).To(Equal(currentDatabase.Generation))
					g.Expect(currentDatabase.Status.ObservedGeneration).To(HaveValue(Equal(currentDatabase.Generation)))
					g.Expect(currentDatabase.Status.CustomMetricsPublication).NotTo(BeNil())
					if currentDatabase.Status.CustomMetricsPublication == nil {
						return
					}
					g.Expect(currentDatabase.Status.CustomMetricsPublication.ObservedGeneration).To(Equal(currentDatabase.Generation))
					contribution, found := databaseContribution(currentDatabase, "appdb")
					g.Expect(found).To(BeTrue())
					g.Expect(contribution.Exists).To(BeFalse())
					g.Expect(contribution.Revision).NotTo(Equal(confirmedRevision))

					currentCluster := &platformv1alpha1.PostgresCluster{}
					g.Expect(kubeClient.Get(ctx, clusterKey, currentCluster)).To(Succeed())
					pgtesthelpers.StopIfPostgresClusterFailed(currentCluster)
					clusterCondition := meta.FindStatusCondition(currentCluster.Status.Conditions, "CustomMetricsReady")
					g.Expect(clusterCondition).NotTo(BeNil())
					if clusterCondition == nil {
						return
					}
					g.Expect(clusterCondition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(clusterCondition.Reason).To(Equal("CustomMetricsReady"))
					g.Expect(clusterCondition.ObservedGeneration).To(Equal(currentCluster.Generation))
					acknowledgement, found := databaseAcknowledgement(currentCluster, currentDatabase, "appdb")
					g.Expect(found).To(BeTrue())
					g.Expect(acknowledgement.DesiredRevision).To(Equal(contribution.Revision))
					g.Expect(acknowledgement.AppliedRevision).To(Equal(contribution.Revision))
					g.Expect(acknowledgement.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(acknowledgement.Reason).To(Equal("CustomMetricsDisabled"))

					generated := &corev1.ConfigMap{}
					g.Expect(kubeClient.Get(ctx, generatedKey, generated)).To(Succeed())
					entries, err := pgtesthelpers.GeneratedMetricEntries(generated)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(entries).To(HaveLen(2))
					g.Expect(entries).NotTo(HaveKey("splunk_operator_database:appdb:application_rows"))
					cnpg := &cnpgv1.Cluster{}
					g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
					g.Expect(cnpg.Status.ConfigMapResourceVersion.Metrics).To(HaveKeyWithValue(generated.Name, generated.ResourceVersion))
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				Eventually(func(g Gomega) {
					families, err := pgtesthelpers.ScrapePostgresMetrics(ctx, kubeClient, metricsClient, clusterKey)
					g.Expect(err).NotTo(HaveOccurred())
					_, found := families[databaseMetricFamily]
					g.Expect(found).To(BeFalse())
					expectClusterCustomMetricSet(g, families, "updated", 13)
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
			},
		)
	})

func clusterMetricA(value int, state string) string {
	return fmt.Sprintf(`platform_probe:
  type: gauge
  help: Platform probe value
  query: SELECT %d::float AS observed_value, '%s'::text AS state
  value: observed_value
  labels:
    - state
`, value, state)
}

func clusterMetricB() string {
	return `secondary_probe:
  type: counter
  help: Secondary probe value
  query: SELECT 23::float AS observed_value
  value: observed_value
`
}

func databaseMetric() string {
	return `application_rows:
  type: gauge
  help: Application fixture row count
  query: |
    SELECT current_database() AS database, count(*)::float AS row_count
    FROM public.custom_metrics_fixture
    GROUP BY current_database()
  value: row_count
  labels:
    - database
`
}

func invalidClusterMetricA() string {
	return `platform_probe:
  type: histogram
  help: Platform probe value
  query: SELECT 13::float AS observed_value, 'updated'::text AS state
  value: observed_value
  labels:
    - state
`
}

func databaseInfo(database *platformv1alpha1.PostgresDatabase, name string) (platformv1alpha1.DatabaseInfo, bool) {
	for _, info := range database.Status.Databases {
		if info.Name == name {
			return info, true
		}
	}
	return platformv1alpha1.DatabaseInfo{}, false
}

func databaseContribution(
	database *platformv1alpha1.PostgresDatabase,
	name string,
) (platformv1alpha1.DatabaseCustomMetricsContribution, bool) {
	if database.Status.CustomMetricsPublication == nil {
		return platformv1alpha1.DatabaseCustomMetricsContribution{}, false
	}
	for _, contribution := range database.Status.CustomMetricsPublication.Contributions {
		if contribution.DatabaseName == name {
			return contribution, true
		}
	}
	return platformv1alpha1.DatabaseCustomMetricsContribution{}, false
}

func databaseAcknowledgement(
	cluster *platformv1alpha1.PostgresCluster,
	database *platformv1alpha1.PostgresDatabase,
	databaseName string,
) (platformv1alpha1.DatabaseCustomMetricsStatus, bool) {
	if cluster.Status.CustomMetricsStatus == nil {
		return platformv1alpha1.DatabaseCustomMetricsStatus{}, false
	}
	for _, acknowledgement := range cluster.Status.CustomMetricsStatus.DatabaseContributions {
		if acknowledgement.PostgresDatabaseName == database.Name &&
			acknowledgement.PostgresDatabaseUID == string(database.UID) &&
			acknowledgement.DatabaseName == databaseName {
			return acknowledgement, true
		}
	}
	return platformv1alpha1.DatabaseCustomMetricsStatus{}, false
}

func cnpgMetricSelectorCount(cluster *cnpgv1.Cluster, configMapName string) int {
	if cluster.Spec.Monitoring == nil {
		return 0
	}
	count := 0
	for _, selector := range cluster.Spec.Monitoring.CustomQueriesConfigMap {
		if selector.Name == configMapName && selector.Key == cnpginfra.MonitoringCMKey {
			count++
		}
	}
	return count
}

func expectActiveCustomMetricSet(g Gomega, families pgtesthelpers.MetricFamilies, state string, platformValue float64) {
	expectClusterCustomMetricSet(g, families, state, platformValue)
	pgtesthelpers.ExpectMetricSample(g, families, databaseMetricFamily, "Application fixture row count", dto.MetricType_GAUGE, map[string]string{"database": "appdb"}, 3)
	_, found, err := pgtesthelpers.MetricSample(families, databaseMetricFamily, map[string]string{"database": "otherdb"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeFalse())
	g.Expect(pgtesthelpers.MetricFamilyHasValue(families, databaseMetricFamily, 9)).To(BeFalse())
}

func expectClusterCustomMetricSet(g Gomega, families pgtesthelpers.MetricFamilies, state string, platformValue float64) {
	pgtesthelpers.ExpectMetricSample(g, families, clusterMetricFamilyA, "Platform probe value", dto.MetricType_GAUGE, map[string]string{"state": state}, platformValue)
	otherState := "initial"
	if state == "initial" {
		otherState = "updated"
	}
	_, found, err := pgtesthelpers.MetricSample(families, clusterMetricFamilyA, map[string]string{"state": otherState})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeFalse())
	pgtesthelpers.ExpectMetricSample(g, families, clusterMetricFamilyB, "Secondary probe value", dto.MetricType_COUNTER, nil, 23)
	g.Expect(families).To(HaveKey(defaultCNPGMetricName))
}
