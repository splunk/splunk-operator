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
	"fmt"
	"strconv"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	databasecore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	connectioninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/splunk/splunk-operator/test/testenv"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SQLCredentials keeps passwords in Secret references rather than Job arguments.
type SQLCredentials struct {
	User              string
	PasswordSecretRef *corev1.SecretKeySelector
}

// ManagedRoleCredentials resolves both managed logins from published database status.
func ManagedRoleCredentials(ctx context.Context, kubeClient client.Client, dbKey client.ObjectKey, database string) (admin, rw SQLCredentials) {
	GinkgoHelper()
	pd := &platformv1alpha1.PostgresDatabase{}
	Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
	for _, info := range pd.Status.Databases {
		if info.Name == database {
			return readRoleSecret(ctx, kubeClient, dbKey.Namespace, info.AdminUserSecretRef),
				readRoleSecret(ctx, kubeClient, dbKey.Namespace, info.RWUserSecretRef)
		}
	}
	Fail(fmt.Sprintf("PostgresDatabase %s publishes no status for database %q", dbKey, database))
	return
}

func readRoleSecret(ctx context.Context, kubeClient client.Client, namespace string, ref *corev1.SecretKeySelector) SQLCredentials {
	GinkgoHelper()
	Expect(ref).NotTo(BeNil(), "database status has no managed role Secret reference")
	selector := ref.DeepCopy()
	if selector.Key == "" {
		selector.Key = "password"
	}
	secret := &corev1.Secret{}
	Expect(kubeClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, secret)).To(Succeed())
	Expect(secret.Data["username"]).NotTo(BeEmpty(), "managed role Secret has no username")
	Expect(secret.Data[selector.Key]).NotTo(BeEmpty(), "managed role Secret has no password")
	return SQLCredentials{User: string(secret.Data["username"]), PasswordSecretRef: selector}
}

// VerifyPostgresDatabaseConnectivity checks both application roles through a
// separate client Pod, the published RW endpoint and verify-full TLS.
func VerifyPostgresDatabaseConnectivity(ctx context.Context, kubeClient client.Client, podClient coreclient.CoreV1Interface, database *platformv1alpha1.PostgresDatabase, clusterKey client.ObjectKey) {
	GinkgoHelper()
	Expect(database.Status.Databases).To(HaveLen(1))
	status := database.Status.Databases[0]
	Expect(status.ConfigMapRef).NotTo(BeNil())
	metadata := &corev1.ConfigMap{}
	Expect(kubeClient.Get(ctx, client.ObjectKey{Namespace: database.Namespace, Name: status.ConfigMapRef.Name}, metadata)).To(Succeed())
	name := metadata.Data[databasecore.ConfigMapKeyDatabaseName]
	Expect(name).To(Equal(status.Name))
	admin, rw := ManagedRoleCredentials(ctx, kubeClient, client.ObjectKeyFromObject(database), name)
	Expect(metadata.Data[databasecore.ConfigMapKeyAdminUser]).To(Equal(admin.User))
	Expect(metadata.Data[databasecore.ConfigMapKeyRWUser]).To(Equal(rw.User))
	Expect(RunSQLAs(ctx, kubeClient, podClient, clusterKey, admin, name,
		"SELECT current_database(), current_user; CREATE TABLE public.sok_connectivity_probe (value text NOT NULL); INSERT INTO public.sok_connectivity_probe VALUES ('admin-write');")).To(Equal(name + "|" + admin.User))
	Expect(RunSQLAs(ctx, kubeClient, podClient, clusterKey, rw, name,
		"INSERT INTO public.sok_connectivity_probe VALUES ('rw-write'); SELECT current_database(), current_user, string_agg(value, ',' ORDER BY value) FROM public.sok_connectivity_probe;")).To(Equal(name + "|" + rw.User + "|admin-write,rw-write"))
	RunSQLAs(ctx, kubeClient, podClient, clusterKey, admin, name, "DROP TABLE public.sok_connectivity_probe")
}

// RunSQLAs is the shared authenticated SQL transport for connectivity and upgrade
// checks. Jobs use the running cluster image and remain available to framework
// failure diagnostics; normal namespace teardown removes successful Jobs.
func RunSQLAs(ctx context.Context, kubeClient client.Client, podClient coreclient.CoreV1Interface, clusterKey client.ObjectKey, creds SQLCredentials, database, sql string) string {
	GinkgoHelper()
	cluster := &platformv1alpha1.PostgresCluster{}
	Expect(kubeClient.Get(ctx, clusterKey, cluster)).To(Succeed())
	Expect(cluster.Status.Resources).NotTo(BeNil())
	Expect(cluster.Status.Resources.ConfigMapRef).NotTo(BeNil())
	config := &corev1.ConfigMap{}
	Expect(kubeClient.Get(ctx, client.ObjectKey{Namespace: clusterKey.Namespace, Name: cluster.Status.Resources.ConfigMapRef.Name}, config)).To(Succeed())
	backend := &cnpgv1.Cluster{}
	Expect(kubeClient.Get(ctx, clusterKey, backend)).To(Succeed())
	Expect(backend.Spec.ImageName).NotTo(BeEmpty())
	Expect(cluster.Spec.Images).NotTo(BeNil())
	job := sqlClientJob(cluster, config, backend.Spec.ImageName, creds, database, sql)
	Expect(kubeClient.Create(ctx, job)).To(Succeed())
	Eventually(ctx, func(g Gomega) {
		current := &batchv1.Job{}
		g.Expect(kubeClient.Get(ctx, client.ObjectKeyFromObject(job), current)).To(Succeed())
		complete, err := connectivityJobResult(current)
		if err != nil {
			StopTrying(fmt.Sprintf("SQL client Job %s/%s failed; inspect its Pod logs", job.Namespace, job.Name)).Wrap(err).Now()
		}
		g.Expect(complete).To(BeTrue())
	}, testenv.ReadinessPollTimeout, testenv.PollInterval).Should(Succeed())
	var output string
	Eventually(ctx, func() error {
		var err error
		output, err = sqlJobOutput(ctx, kubeClient, podClient, job)
		return err
	}, testenv.ReadinessPollTimeout, testenv.PollInterval).Should(Succeed())
	return output
}

// sqlJobOutput reads only a completed Pod owned by this Job, without spawning a CLI.
func sqlJobOutput(ctx context.Context, kubeClient client.Client, podsClient coreclient.CoreV1Interface, job *batchv1.Job) (string, error) {
	pods := &corev1.PodList{}
	if err := kubeClient.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels{batchv1.ControllerUidLabel: string(job.UID)}); err != nil {
		return "", err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded && metav1.IsControlledBy(&pod, job) {
			output, err := podsClient.Pods(job.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "psql"}).DoRaw(ctx)
			return strings.TrimSpace(string(output)), err
		}
	}
	return "", fmt.Errorf("no completed Pod owned by SQL client Job %s/%s", job.Namespace, job.Name)
}

func connectivityJobResult(job *batchv1.Job) (bool, error) {
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		if condition.Type == batchv1.JobFailed {
			return false, fmt.Errorf("Job reported %s", condition.Reason)
		}
		if condition.Type == batchv1.JobComplete {
			return true, nil
		}
	}
	return false, nil
}

func sqlClientJob(cluster *platformv1alpha1.PostgresCluster, config *corev1.ConfigMap, image string, creds SQLCredentials, database, sql string) *batchv1.Job {
	GinkgoHelper()
	caName, caKey, err := connectioninfo.ParseSecretReference(config.Data[connectioninfo.KeyServerCASecretRef])
	Expect(err).To(Succeed())
	configEnv := func(name, key string) corev1.EnvVar {
		return corev1.EnvVar{
			Name:      name,
			ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: config.Name}, Key: key}},
		}
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "postgres-sql-", Namespace: cluster.Namespace},
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptr.To(int32(0)),
			ActiveDeadlineSeconds: ptr.To(int64(testenv.ReadinessPollTimeout.Seconds())),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy:                corev1.RestartPolicyNever,
				AutomountServiceAccountToken: ptr.To(false),
				ImagePullSecrets:             cluster.Spec.Images.ImagePullSecrets,
				Containers: []corev1.Container{{
					Name:    "psql",
					Image:   image,
					Command: []string{"psql", "-X", "-w", "-A", "-t", "-q", "-v", "ON_ERROR_STOP=1", "-c", sql},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						ReadOnlyRootFilesystem:   ptr.To(true),
						RunAsNonRoot:             ptr.To(true),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Env: []corev1.EnvVar{
						configEnv("PGHOST", connectioninfo.KeyClusterRWEndpoint),
						configEnv("PGPORT", connectioninfo.KeyDefaultClusterPort),
						{Name: "PGDATABASE", Value: database},
						{Name: "PGUSER", Value: creds.User},
						{Name: "PGPASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: creds.PasswordSecretRef}},
						{Name: "PGSSLMODE", Value: "verify-full"},
						{Name: "PGSSLROOTCERT", Value: "/etc/postgres-ca/ca.crt"},
						{Name: "PGOPTIONS", Value: "-c client_min_messages=warning"},
						{Name: "PGCONNECT_TIMEOUT", Value: strconv.Itoa(int(testenv.KubectlQuickTimeout.Seconds()))},
					},
					VolumeMounts: []corev1.VolumeMount{{Name: "server-ca", MountPath: "/etc/postgres-ca", ReadOnly: true}},
				}},
				Volumes: []corev1.Volume{{
					Name: "server-ca",
					VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
						SecretName: caName,
						Items:      []corev1.KeyToPath{{Key: caKey, Path: "ca.crt"}},
					}},
				}},
			}},
		},
	}
}
