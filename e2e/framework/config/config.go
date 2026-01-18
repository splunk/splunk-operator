package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config controls the E2E runner behavior.
type Config struct {
	RunID                         string
	SpecDir                       string
	DatasetRegistry               string
	ArtifactDir                   string
	IncludeTags                   []string
	ExcludeTags                   []string
	Capabilities                  []string
	Parallelism                   int
	Kubeconfig                    string
	NamespacePrefix               string
	OperatorImage                 string
	SplunkImage                   string
	OperatorNamespace             string
	OperatorDeployment            string
	ClusterProvider               string
	ClusterWide                   bool
	LogFormat                     string
	LogLevel                      string
	MetricsEnabled                bool
	MetricsPath                   string
	GraphEnabled                  bool
	DefaultTimeout                time.Duration
	SkipTeardown                  bool
	TopologyMode                  string
	LogCollection                 string
	SplunkLogTail                 int
	ObjectStoreProvider           string
	ObjectStoreBucket             string
	ObjectStorePrefix             string
	ObjectStoreRegion             string
	ObjectStoreEndpoint           string
	ObjectStoreAccessKey          string
	ObjectStoreSecretKey          string
	ObjectStoreSessionToken       string
	ObjectStoreS3PathStyle        bool
	ObjectStoreGCPProject         string
	ObjectStoreGCPCredentialsFile string
	ObjectStoreGCPCredentialsJSON string
	ObjectStoreAzureAccount       string
	ObjectStoreAzureKey           string
	ObjectStoreAzureEndpoint      string
	ObjectStoreAzureSASToken      string
	OTelEnabled                   bool
	OTelEndpoint                  string
	OTelHeaders                   string
	OTelInsecure                  bool
	OTelServiceName               string
	OTelResourceAttrs             string
	Neo4jEnabled                  bool
	Neo4jURI                      string
	Neo4jUser                     string
	Neo4jPassword                 string
	Neo4jDatabase                 string
}

// Load parses flags and environment variables into Config.
func Load() *Config {
	cwd, _ := os.Getwd()
	defaultRunID := time.Now().UTC().Format("20060102T150405Z")
	defaultArtifacts := filepath.Join(cwd, "e2e", "artifacts", defaultRunID)
	defaultMetrics := filepath.Join(defaultArtifacts, "metrics.prom")

	cfg := &Config{}
	flag.StringVar(&cfg.RunID, "run-id", envOrDefault("E2E_RUN_ID", defaultRunID), "unique run identifier")
	flag.StringVar(&cfg.SpecDir, "spec-dir", envOrDefault("E2E_SPEC_DIR", filepath.Join(cwd, "e2e", "specs")), "directory containing test specs")
	flag.StringVar(&cfg.DatasetRegistry, "dataset-registry", envOrDefault("E2E_DATASET_REGISTRY", filepath.Join(cwd, "e2e", "datasets", "datf-datasets.yaml")), "path to dataset registry YAML")
	flag.StringVar(&cfg.ArtifactDir, "artifact-dir", envOrDefault("E2E_ARTIFACT_DIR", defaultArtifacts), "directory for artifacts")
	flag.IntVar(&cfg.Parallelism, "parallel", envOrDefaultInt("E2E_PARALLEL", 1), "max parallel tests")
	if flag.Lookup("kubeconfig") == nil {
		flag.StringVar(&cfg.Kubeconfig, "kubeconfig", envOrDefault("KUBECONFIG", ""), "path to kubeconfig")
	} else {
		cfg.Kubeconfig = envOrDefault("KUBECONFIG", "")
	}
	flag.StringVar(&cfg.NamespacePrefix, "namespace-prefix", envOrDefault("E2E_NAMESPACE_PREFIX", "e2e"), "namespace prefix for tests")
	flag.StringVar(&cfg.OperatorImage, "operator-image", envOrDefault("SPLUNK_OPERATOR_IMAGE", "splunk/splunk-operator:3.0.0"), "splunk operator image")
	flag.StringVar(&cfg.SplunkImage, "splunk-image", envOrDefault("SPLUNK_ENTERPRISE_IMAGE", "splunk/splunk:10.0.0"), "splunk enterprise image")
	flag.StringVar(&cfg.OperatorNamespace, "operator-namespace", envOrDefault("E2E_OPERATOR_NAMESPACE", "splunk-operator"), "operator namespace")
	flag.StringVar(&cfg.OperatorDeployment, "operator-deployment", envOrDefault("E2E_OPERATOR_DEPLOYMENT", "splunk-operator-controller-manager"), "operator deployment name")
	flag.StringVar(&cfg.ClusterProvider, "cluster-provider", envOrDefault("CLUSTER_PROVIDER", "kind"), "cluster provider name")
	flag.BoolVar(&cfg.ClusterWide, "cluster-wide", envOrDefaultBool("CLUSTER_WIDE", false), "install operator cluster-wide")
	flag.StringVar(&cfg.LogFormat, "log-format", envOrDefault("E2E_LOG_FORMAT", "json"), "log format: json|console")
	flag.StringVar(&cfg.LogLevel, "log-level", envOrDefault("E2E_LOG_LEVEL", "info"), "log level: debug|info|warn|error")
	flag.BoolVar(&cfg.MetricsEnabled, "metrics", envOrDefaultBool("E2E_METRICS", true), "enable metrics output")
	flag.StringVar(&cfg.MetricsPath, "metrics-path", envOrDefault("E2E_METRICS_PATH", defaultMetrics), "metrics output path")
	flag.BoolVar(&cfg.GraphEnabled, "graph", envOrDefaultBool("E2E_GRAPH", true), "enable knowledge graph output")
	flag.DurationVar(&cfg.DefaultTimeout, "default-timeout", envOrDefaultDuration("E2E_DEFAULT_TIMEOUT", 90*time.Minute), "default test timeout")
	flag.BoolVar(&cfg.SkipTeardown, "skip-teardown", envOrDefaultBool("E2E_SKIP_TEARDOWN", false), "skip namespace teardown after tests")
	flag.StringVar(&cfg.TopologyMode, "topology-mode", envOrDefault("E2E_TOPOLOGY_MODE", "suite"), "topology mode: suite|test")
	flag.StringVar(&cfg.LogCollection, "log-collection", envOrDefault("E2E_LOG_COLLECTION", "failure"), "log collection: never|failure|always")
	flag.IntVar(&cfg.SplunkLogTail, "splunk-log-tail", envOrDefaultInt("E2E_SPLUNK_LOG_TAIL", 0), "tail N lines of Splunk internal logs (0=all)")
	flag.StringVar(&cfg.ObjectStoreProvider, "objectstore-provider", envOrDefault("E2E_OBJECTSTORE_PROVIDER", ""), "object store provider: s3|gcs|azure")
	flag.StringVar(&cfg.ObjectStoreBucket, "objectstore-bucket", envOrDefault("E2E_OBJECTSTORE_BUCKET", ""), "object store bucket/container")
	flag.StringVar(&cfg.ObjectStorePrefix, "objectstore-prefix", envOrDefault("E2E_OBJECTSTORE_PREFIX", ""), "object store prefix")
	flag.StringVar(&cfg.ObjectStoreRegion, "objectstore-region", envOrDefault("E2E_OBJECTSTORE_REGION", ""), "object store region")
	flag.StringVar(&cfg.ObjectStoreEndpoint, "objectstore-endpoint", envOrDefault("E2E_OBJECTSTORE_ENDPOINT", ""), "object store endpoint override")
	flag.StringVar(&cfg.ObjectStoreAccessKey, "objectstore-access-key", envOrDefault("E2E_OBJECTSTORE_ACCESS_KEY", ""), "object store access key")
	flag.StringVar(&cfg.ObjectStoreSecretKey, "objectstore-secret-key", envOrDefault("E2E_OBJECTSTORE_SECRET_KEY", ""), "object store secret key")
	flag.StringVar(&cfg.ObjectStoreSessionToken, "objectstore-session-token", envOrDefault("E2E_OBJECTSTORE_SESSION_TOKEN", ""), "object store session token")
	flag.BoolVar(&cfg.ObjectStoreS3PathStyle, "objectstore-s3-path-style", envOrDefaultBool("E2E_OBJECTSTORE_S3_PATH_STYLE", false), "use S3 path-style addressing")
	flag.StringVar(&cfg.ObjectStoreGCPProject, "objectstore-gcp-project", envOrDefault("E2E_OBJECTSTORE_GCP_PROJECT", ""), "GCP project ID")
	flag.StringVar(&cfg.ObjectStoreGCPCredentialsFile, "objectstore-gcp-credentials-file", envOrDefault("E2E_OBJECTSTORE_GCP_CREDENTIALS_FILE", ""), "GCP credentials file path")
	flag.StringVar(&cfg.ObjectStoreGCPCredentialsJSON, "objectstore-gcp-credentials-json", envOrDefault("E2E_OBJECTSTORE_GCP_CREDENTIALS_JSON", ""), "GCP credentials JSON")
	flag.StringVar(&cfg.ObjectStoreAzureAccount, "objectstore-azure-account", envOrDefault("E2E_OBJECTSTORE_AZURE_ACCOUNT", ""), "Azure storage account name")
	flag.StringVar(&cfg.ObjectStoreAzureKey, "objectstore-azure-key", envOrDefault("E2E_OBJECTSTORE_AZURE_KEY", ""), "Azure storage account key")
	flag.StringVar(&cfg.ObjectStoreAzureEndpoint, "objectstore-azure-endpoint", envOrDefault("E2E_OBJECTSTORE_AZURE_ENDPOINT", ""), "Azure blob endpoint override")
	flag.StringVar(&cfg.ObjectStoreAzureSASToken, "objectstore-azure-sas-token", envOrDefault("E2E_OBJECTSTORE_AZURE_SAS_TOKEN", ""), "Azure SAS token")
	flag.BoolVar(&cfg.OTelEnabled, "otel", envOrDefaultBool("E2E_OTEL_ENABLED", false), "enable OpenTelemetry exporters")
	flag.StringVar(&cfg.OTelEndpoint, "otel-endpoint", envOrDefault("E2E_OTEL_ENDPOINT", ""), "OTLP endpoint (host:port)")
	flag.StringVar(&cfg.OTelHeaders, "otel-headers", envOrDefault("E2E_OTEL_HEADERS", ""), "OTLP headers as comma-separated key=value pairs")
	flag.BoolVar(&cfg.OTelInsecure, "otel-insecure", envOrDefaultBool("E2E_OTEL_INSECURE", true), "disable TLS for OTLP endpoint")
	flag.StringVar(&cfg.OTelServiceName, "otel-service-name", envOrDefault("E2E_OTEL_SERVICE_NAME", "splunk-operator-e2e"), "OTel service name")
	flag.StringVar(&cfg.OTelResourceAttrs, "otel-resource-attrs", envOrDefault("E2E_OTEL_RESOURCE_ATTRS", ""), "extra OTel resource attributes key=value pairs")
	flag.BoolVar(&cfg.Neo4jEnabled, "neo4j", envOrDefaultBool("E2E_NEO4J_ENABLED", false), "enable Neo4j export")
	flag.StringVar(&cfg.Neo4jURI, "neo4j-uri", envOrDefault("E2E_NEO4J_URI", ""), "Neo4j connection URI")
	flag.StringVar(&cfg.Neo4jUser, "neo4j-user", envOrDefault("E2E_NEO4J_USER", ""), "Neo4j username")
	flag.StringVar(&cfg.Neo4jPassword, "neo4j-password", envOrDefault("E2E_NEO4J_PASSWORD", ""), "Neo4j password")
	flag.StringVar(&cfg.Neo4jDatabase, "neo4j-database", envOrDefault("E2E_NEO4J_DATABASE", "neo4j"), "Neo4j database name")

	includeTags := flag.String("include-tags", envOrDefault("E2E_INCLUDE_TAGS", ""), "comma-separated tag allowlist")
	excludeTags := flag.String("exclude-tags", envOrDefault("E2E_EXCLUDE_TAGS", ""), "comma-separated tag denylist")
	capabilities := flag.String("capabilities", envOrDefault("E2E_CAPABILITIES", ""), "comma-separated capability list")

	flag.Parse()
	cfg.IncludeTags = splitCSV(*includeTags)
	cfg.ExcludeTags = splitCSV(*excludeTags)
	cfg.Capabilities = splitCSV(*capabilities)
	if existing := flag.Lookup("kubeconfig"); existing != nil {
		if value := strings.TrimSpace(existing.Value.String()); value != "" {
			cfg.Kubeconfig = value
		}
	}
	if cfg.MetricsPath == defaultMetrics {
		cfg.MetricsPath = filepath.Join(cfg.ArtifactDir, "metrics.prom")
	}
	if cfg.Parallelism < 1 {
		cfg.Parallelism = 1
	}
	if !cfg.Neo4jEnabled && cfg.Neo4jURI != "" && os.Getenv("E2E_NEO4J_ENABLED") == "" {
		cfg.Neo4jEnabled = true
	}

	return cfg
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envOrDefaultInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed := 0
	_, err := fmt.Sscanf(value, "%d", &parsed)
	if err != nil {
		return fallback
	}
	return parsed
}

func envOrDefaultBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	default:
		return fallback
	}
}

func envOrDefaultDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func splitCSV(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
