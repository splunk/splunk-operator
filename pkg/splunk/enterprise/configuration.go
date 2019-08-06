package enterprise

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math/rand"
	"time"
	"fmt"
)

const (
	SPLUNK_SECRET_BYTES = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890%()*+,-./<=>?@[]^_{|}~"
	SPLUNK_SECRET_LEN = 24
)

func init() {
	// seed rng for splunk secret generation
	rand.Seed(time.Now().UnixNano())
}


func GetSplunkAppLabels(identifier string, typeLabel string) map[string]string {
	labels := map[string]string{
		"app": "splunk",
		"for": identifier,
		"type": typeLabel,
	}

	return labels
}


func GetSplunkPorts() map[string]int {
	return map[string]int{
		"splunkweb": 8000,
		"splunkd": 8089,
		"dfccontrol": 17000,
		"datarecieve": 19000,
		"dfsmaster": 9000,
	}
}


func GetSplunkContainerPorts() []corev1.ContainerPort {
	l := []corev1.ContainerPort{}
	for key, value := range GetSplunkPorts() {
		l = append(l, corev1.ContainerPort{
			Name: key,
			ContainerPort: int32(value),
		})
	}
	return l
}


func GetSplunkServicePorts() []corev1.ServicePort {
	l := []corev1.ServicePort{}
	for key, value := range GetSplunkPorts() {
		l = append(l, corev1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}


func AppendSplunkDfsOverrides(cr *v1alpha1.SplunkEnterprise, overrides map[string]string) {
	// assertion: overrides is not nil
	if cr.Spec.EnableDFS {
		overrides["SPLUNK_ENABLE_DFS"] = "true"
		overrides["SPARK_MASTER_HOST"] = spark.GetSparkServiceName(spark.SPARK_MASTER, GetIdentifier(cr))
		overrides["SPARK_MASTER_WEBUI_PORT"] = "8009"
		overrides["SPARK_HOME"] = "/mnt/splunk-spark"
		overrides["JAVA_HOME"] = "/mnt/splunk-jdk"
		if cr.Spec.Topology.SearchHeads > 1 {
			overrides["SPLUNK_DFW_NUM_SLOTS_ENABLED"] = "true"
		} else {
			overrides["SPLUNK_DFW_NUM_SLOTS_ENABLED"] = "false"
		}
	}
}


func GetSplunkConfiguration(cr *v1alpha1.SplunkEnterprise, overrides map[string]string) []corev1.EnvVar {
	splunk_defaults := "/mnt/splunk-secrets/default.yml"
	if cr.Spec.DefaultsUrl != "" {
		splunk_defaults = fmt.Sprintf("%s,%s", splunk_defaults, cr.Spec.DefaultsUrl)
	}
	if cr.Spec.Defaults != "" {
		splunk_defaults = fmt.Sprintf("%s,%s", splunk_defaults, "/mnt/splunk-defaults/default.yml")
	}

	conf := []corev1.EnvVar{
		{
			Name: "SPLUNK_HOME",
			Value: "/opt/splunk",
		},
		{
			Name: "SPLUNK_START_ARGS",
			Value: "--accept-license",
		},
		{
			Name: "SPLUNK_DEFAULTS_URL",
			Value: splunk_defaults,
		},
	}

	if overrides != nil {
		for k, v := range overrides {
			conf = append(conf, corev1.EnvVar{
				Name: k,
				Value: v,
			})
		}
	}

	return conf
}


func GetSplunkClusterConfiguration(cr *v1alpha1.SplunkEnterprise, searchHeadCluster bool, overrides map[string]string) []corev1.EnvVar {
	identifier := GetIdentifier(cr)
	namespace := GetNamespace(cr)

	urls := []corev1.EnvVar{
		{
			Name: "SPLUNK_CLUSTER_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_CLUSTER_MASTER, identifier),
		},{
			Name: "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(namespace, SPLUNK_INDEXER, identifier, cr.Spec.Topology.Indexers, false),
		},{
			Name: "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_LICENSE_MASTER, identifier),
		},
	}

	searchHeadConf := []corev1.EnvVar{
		{
			Name: "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(namespace, SPLUNK_SEARCH_HEAD, identifier, cr.Spec.Topology.SearchHeads, false),
		},
	}

	if searchHeadCluster {
		searchHeadConf = append(searchHeadConf, corev1.EnvVar{
			Name: "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: GetSplunkStatefulsetUrl(namespace, SPLUNK_SEARCH_HEAD, identifier, 0, false),
		})
		searchHeadConf = append(searchHeadConf, corev1.EnvVar{
			Name: "SPLUNK_DEPLOYER_URL",
			Value: GetSplunkServiceName(SPLUNK_DEPLOYER, identifier),
		})
	}

	return append(append(urls, searchHeadConf...), GetSplunkConfiguration(cr, overrides)...)
}


func GetSplunkDefaults(cr *v1alpha1.SplunkEnterprise) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetSplunkDefaultsName(GetIdentifier(cr)),
			Namespace: GetNamespace(cr),
		},
		Data: map[string]string{
			"default.yml" : cr.Spec.Defaults,
		},
	}
}


func GetSplunkSecrets(cr *v1alpha1.SplunkEnterprise) *corev1.Secret {
	// generate some default secret values to share across the cluster
	secretData := map[string][]byte {
		"password" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
		"hec_token" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
		"idc_secret" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
		"shc_secret" : GenerateSplunkSecret(SPLUNK_SECRET_LEN),
	}
	secretData["default.yml"] = []byte(fmt.Sprintf(`
splunk:
    password: "%s"
    hec_token: "%s"
    idc:
        secret: "%s"
    shc:
        secret: "%s"
`,
		secretData["password"],
		secretData["hec_token"],
		secretData["idc_secret"],
		secretData["shc_secret"]))

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetSplunkSecretsName(GetIdentifier(cr)),
			Namespace: GetNamespace(cr),
		},
		Data: secretData,
	}
}


func GenerateSplunkSecret(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = SPLUNK_SECRET_BYTES[rand.Int63() % int64(len(SPLUNK_SECRET_BYTES))]
	}
	return b
}
