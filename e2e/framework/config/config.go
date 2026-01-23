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
	SplunkdEndpoint               string
	SplunkdUsername               string
	SplunkdPassword               string
	SplunkdInsecure               bool
	SplunkdMgmtPort               int
	SplunkdHECPort                int
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
	ProgressInterval              time.Duration
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

	cfg := defaultConfig(cwd, defaultRunID, defaultArtifacts, defaultMetrics)

	configPath := detectConfigPath(os.Args, os.Getenv("E2E_CONFIG"))
	if configPath != "" {
		fileCfg, err := loadFileConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load config file %s: %v\n", configPath, err)
			os.Exit(1)
		}
		if err := applyFileConfig(cfg, fileCfg); err != nil {
			fmt.Fprintf(os.Stderr, "failed to apply config file %s: %v\n", configPath, err)
			os.Exit(1)
		}
	}

	flag.StringVar(&configPath, "config", configPath, "path to config file")
	flag.StringVar(&cfg.RunID, "run-id", envOrDefault("E2E_RUN_ID", cfg.RunID), "unique run identifier")
	flag.StringVar(&cfg.SpecDir, "spec-dir", envOrDefault("E2E_SPEC_DIR", cfg.SpecDir), "directory containing test specs")
	flag.StringVar(&cfg.DatasetRegistry, "dataset-registry", envOrDefault("E2E_DATASET_REGISTRY", cfg.DatasetRegistry), "path to dataset registry YAML")
	flag.StringVar(&cfg.ArtifactDir, "artifact-dir", envOrDefault("E2E_ARTIFACT_DIR", cfg.ArtifactDir), "directory for artifacts")
	flag.IntVar(&cfg.Parallelism, "parallel", envOrDefaultInt("E2E_PARALLEL", cfg.Parallelism), "max parallel tests")
	if flag.Lookup("kubeconfig") == nil {
		flag.StringVar(&cfg.Kubeconfig, "kubeconfig", envOrDefault("KUBECONFIG", cfg.Kubeconfig), "path to kubeconfig")
	} else {
		cfg.Kubeconfig = envOrDefault("KUBECONFIG", cfg.Kubeconfig)
	}
	flag.StringVar(&cfg.NamespacePrefix, "namespace-prefix", envOrDefault("E2E_NAMESPACE_PREFIX", cfg.NamespacePrefix), "namespace prefix for tests")
	flag.StringVar(&cfg.OperatorImage, "operator-image", envOrDefault("SPLUNK_OPERATOR_IMAGE", cfg.OperatorImage), "splunk operator image")
	flag.StringVar(&cfg.SplunkImage, "splunk-image", envOrDefault("SPLUNK_ENTERPRISE_IMAGE", cfg.SplunkImage), "splunk enterprise image")
	flag.StringVar(&cfg.SplunkdEndpoint, "splunkd-endpoint", envOrDefault("E2E_SPLUNKD_ENDPOINT", cfg.SplunkdEndpoint), "external Splunkd endpoint (https://host:port)")
	flag.StringVar(&cfg.SplunkdUsername, "splunkd-username", envOrDefault("E2E_SPLUNKD_USERNAME", cfg.SplunkdUsername), "external Splunkd username")
	flag.StringVar(&cfg.SplunkdPassword, "splunkd-password", envOrDefault("E2E_SPLUNKD_PASSWORD", cfg.SplunkdPassword), "external Splunkd password")
	flag.BoolVar(&cfg.SplunkdInsecure, "splunkd-insecure", envOrDefaultBool("E2E_SPLUNKD_INSECURE", cfg.SplunkdInsecure), "skip TLS verification for external Splunkd")
	flag.IntVar(&cfg.SplunkdMgmtPort, "splunkd-mgmt-port", envOrDefaultInt("E2E_SPLUNKD_MGMT_PORT", cfg.SplunkdMgmtPort), "external Splunkd management port")
	flag.IntVar(&cfg.SplunkdHECPort, "splunkd-hec-port", envOrDefaultInt("E2E_SPLUNKD_HEC_PORT", cfg.SplunkdHECPort), "external Splunkd HEC port")
	flag.StringVar(&cfg.OperatorNamespace, "operator-namespace", envOrDefault("E2E_OPERATOR_NAMESPACE", cfg.OperatorNamespace), "operator namespace")
	flag.StringVar(&cfg.OperatorDeployment, "operator-deployment", envOrDefault("E2E_OPERATOR_DEPLOYMENT", cfg.OperatorDeployment), "operator deployment name")
	flag.StringVar(&cfg.ClusterProvider, "cluster-provider", envOrDefault("CLUSTER_PROVIDER", cfg.ClusterProvider), "cluster provider name")
	flag.BoolVar(&cfg.ClusterWide, "cluster-wide", envOrDefaultBool("CLUSTER_WIDE", cfg.ClusterWide), "install operator cluster-wide")
	flag.StringVar(&cfg.LogFormat, "log-format", envOrDefault("E2E_LOG_FORMAT", cfg.LogFormat), "log format: json|console")
	flag.StringVar(&cfg.LogLevel, "log-level", envOrDefault("E2E_LOG_LEVEL", cfg.LogLevel), "log level: debug|info|warn|error")
	flag.BoolVar(&cfg.MetricsEnabled, "metrics", envOrDefaultBool("E2E_METRICS", cfg.MetricsEnabled), "enable metrics output")
	flag.StringVar(&cfg.MetricsPath, "metrics-path", envOrDefault("E2E_METRICS_PATH", cfg.MetricsPath), "metrics output path")
	flag.BoolVar(&cfg.GraphEnabled, "graph", envOrDefaultBool("E2E_GRAPH", cfg.GraphEnabled), "enable knowledge graph output")
	flag.DurationVar(&cfg.DefaultTimeout, "default-timeout", envOrDefaultDuration("E2E_DEFAULT_TIMEOUT", cfg.DefaultTimeout), "default test timeout")
	flag.DurationVar(&cfg.ProgressInterval, "progress-interval", envOrDefaultDuration("E2E_PROGRESS_INTERVAL", cfg.ProgressInterval), "interval for progress logging (0 disables)")
	flag.BoolVar(&cfg.SkipTeardown, "skip-teardown", envOrDefaultBool("E2E_SKIP_TEARDOWN", cfg.SkipTeardown), "skip namespace teardown after tests")
	flag.StringVar(&cfg.TopologyMode, "topology-mode", envOrDefault("E2E_TOPOLOGY_MODE", cfg.TopologyMode), "topology mode: suite|test")
	flag.StringVar(&cfg.LogCollection, "log-collection", envOrDefault("E2E_LOG_COLLECTION", cfg.LogCollection), "log collection: never|failure|always")
	flag.IntVar(&cfg.SplunkLogTail, "splunk-log-tail", envOrDefaultInt("E2E_SPLUNK_LOG_TAIL", cfg.SplunkLogTail), "tail N lines of Splunk internal logs (0=all)")
	flag.StringVar(&cfg.ObjectStoreProvider, "objectstore-provider", envOrDefault("E2E_OBJECTSTORE_PROVIDER", cfg.ObjectStoreProvider), "object store provider: s3|gcs|azure")
	flag.StringVar(&cfg.ObjectStoreBucket, "objectstore-bucket", envOrDefault("E2E_OBJECTSTORE_BUCKET", cfg.ObjectStoreBucket), "object store bucket/container")
	flag.StringVar(&cfg.ObjectStorePrefix, "objectstore-prefix", envOrDefault("E2E_OBJECTSTORE_PREFIX", cfg.ObjectStorePrefix), "object store prefix")
	flag.StringVar(&cfg.ObjectStoreRegion, "objectstore-region", envOrDefault("E2E_OBJECTSTORE_REGION", cfg.ObjectStoreRegion), "object store region")
	flag.StringVar(&cfg.ObjectStoreEndpoint, "objectstore-endpoint", envOrDefault("E2E_OBJECTSTORE_ENDPOINT", cfg.ObjectStoreEndpoint), "object store endpoint override")
	flag.StringVar(&cfg.ObjectStoreAccessKey, "objectstore-access-key", envOrDefault("E2E_OBJECTSTORE_ACCESS_KEY", cfg.ObjectStoreAccessKey), "object store access key")
	flag.StringVar(&cfg.ObjectStoreSecretKey, "objectstore-secret-key", envOrDefault("E2E_OBJECTSTORE_SECRET_KEY", cfg.ObjectStoreSecretKey), "object store secret key")
	flag.StringVar(&cfg.ObjectStoreSessionToken, "objectstore-session-token", envOrDefault("E2E_OBJECTSTORE_SESSION_TOKEN", cfg.ObjectStoreSessionToken), "object store session token")
	flag.BoolVar(&cfg.ObjectStoreS3PathStyle, "objectstore-s3-path-style", envOrDefaultBool("E2E_OBJECTSTORE_S3_PATH_STYLE", cfg.ObjectStoreS3PathStyle), "use S3 path-style addressing")
	flag.StringVar(&cfg.ObjectStoreGCPProject, "objectstore-gcp-project", envOrDefault("E2E_OBJECTSTORE_GCP_PROJECT", cfg.ObjectStoreGCPProject), "GCP project ID")
	flag.StringVar(&cfg.ObjectStoreGCPCredentialsFile, "objectstore-gcp-credentials-file", envOrDefault("E2E_OBJECTSTORE_GCP_CREDENTIALS_FILE", cfg.ObjectStoreGCPCredentialsFile), "GCP credentials file path")
	flag.StringVar(&cfg.ObjectStoreGCPCredentialsJSON, "objectstore-gcp-credentials-json", envOrDefault("E2E_OBJECTSTORE_GCP_CREDENTIALS_JSON", cfg.ObjectStoreGCPCredentialsJSON), "GCP credentials JSON")
	flag.StringVar(&cfg.ObjectStoreAzureAccount, "objectstore-azure-account", envOrDefault("E2E_OBJECTSTORE_AZURE_ACCOUNT", cfg.ObjectStoreAzureAccount), "Azure storage account name")
	flag.StringVar(&cfg.ObjectStoreAzureKey, "objectstore-azure-key", envOrDefault("E2E_OBJECTSTORE_AZURE_KEY", cfg.ObjectStoreAzureKey), "Azure storage account key")
	flag.StringVar(&cfg.ObjectStoreAzureEndpoint, "objectstore-azure-endpoint", envOrDefault("E2E_OBJECTSTORE_AZURE_ENDPOINT", cfg.ObjectStoreAzureEndpoint), "Azure blob endpoint override")
	flag.StringVar(&cfg.ObjectStoreAzureSASToken, "objectstore-azure-sas-token", envOrDefault("E2E_OBJECTSTORE_AZURE_SAS_TOKEN", cfg.ObjectStoreAzureSASToken), "Azure SAS token")
	flag.BoolVar(&cfg.OTelEnabled, "otel", envOrDefaultBool("E2E_OTEL_ENABLED", cfg.OTelEnabled), "enable OpenTelemetry exporters")
	flag.StringVar(&cfg.OTelEndpoint, "otel-endpoint", envOrDefault("E2E_OTEL_ENDPOINT", cfg.OTelEndpoint), "OTLP endpoint (host:port)")
	flag.StringVar(&cfg.OTelHeaders, "otel-headers", envOrDefault("E2E_OTEL_HEADERS", cfg.OTelHeaders), "OTLP headers as comma-separated key=value pairs")
	flag.BoolVar(&cfg.OTelInsecure, "otel-insecure", envOrDefaultBool("E2E_OTEL_INSECURE", cfg.OTelInsecure), "disable TLS for OTLP endpoint")
	flag.StringVar(&cfg.OTelServiceName, "otel-service-name", envOrDefault("E2E_OTEL_SERVICE_NAME", cfg.OTelServiceName), "OTel service name")
	flag.StringVar(&cfg.OTelResourceAttrs, "otel-resource-attrs", envOrDefault("E2E_OTEL_RESOURCE_ATTRS", cfg.OTelResourceAttrs), "extra OTel resource attributes key=value pairs")
	flag.BoolVar(&cfg.Neo4jEnabled, "neo4j", envOrDefaultBool("E2E_NEO4J_ENABLED", cfg.Neo4jEnabled), "enable Neo4j export")
	flag.StringVar(&cfg.Neo4jURI, "neo4j-uri", envOrDefault("E2E_NEO4J_URI", cfg.Neo4jURI), "Neo4j connection URI")
	flag.StringVar(&cfg.Neo4jUser, "neo4j-user", envOrDefault("E2E_NEO4J_USER", cfg.Neo4jUser), "Neo4j username")
	flag.StringVar(&cfg.Neo4jPassword, "neo4j-password", envOrDefault("E2E_NEO4J_PASSWORD", cfg.Neo4jPassword), "Neo4j password")
	flag.StringVar(&cfg.Neo4jDatabase, "neo4j-database", envOrDefault("E2E_NEO4J_DATABASE", cfg.Neo4jDatabase), "Neo4j database name")

	includeDefault := strings.Join(cfg.IncludeTags, ",")
	excludeDefault := strings.Join(cfg.ExcludeTags, ",")
	capabilitiesDefault := strings.Join(cfg.Capabilities, ",")
	includeTags := flag.String("include-tags", envOrDefault("E2E_INCLUDE_TAGS", includeDefault), "comma-separated tag allowlist")
	excludeTags := flag.String("exclude-tags", envOrDefault("E2E_EXCLUDE_TAGS", excludeDefault), "comma-separated tag denylist")
	capabilities := flag.String("capabilities", envOrDefault("E2E_CAPABILITIES", capabilitiesDefault), "comma-separated capability list")

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

func defaultConfig(cwd, defaultRunID, defaultArtifacts, defaultMetrics string) *Config {
	return &Config{
		RunID:               defaultRunID,
		SpecDir:             filepath.Join(cwd, "e2e", "specs"),
		DatasetRegistry:     filepath.Join(cwd, "e2e", "datasets", "datf-datasets.yaml"),
		ArtifactDir:         defaultArtifacts,
		Parallelism:         1,
		NamespacePrefix:     "e2e",
		OperatorImage:       "splunk/splunk-operator:3.0.0",
		SplunkImage:         "splunk/splunk:10.0.0",
		SplunkdUsername:     "admin",
		SplunkdInsecure:     true,
		SplunkdMgmtPort:     8089,
		SplunkdHECPort:      8088,
		OperatorNamespace:   "splunk-operator",
		OperatorDeployment:  "splunk-operator-controller-manager",
		ClusterProvider:     "kind",
		ClusterWide:         false,
		LogFormat:           "json",
		LogLevel:            "info",
		MetricsEnabled:      true,
		MetricsPath:         defaultMetrics,
		GraphEnabled:        true,
		DefaultTimeout:      90 * time.Minute,
		ProgressInterval:    30 * time.Second,
		SkipTeardown:        false,
		TopologyMode:        "suite",
		LogCollection:       "failure",
		SplunkLogTail:       0,
		ObjectStoreProvider: "",
		ObjectStoreBucket:   "",
		ObjectStorePrefix:   "",
		ObjectStoreRegion:   "",
		ObjectStoreEndpoint: "",
		OTelEnabled:         false,
		OTelInsecure:        true,
		OTelServiceName:     "splunk-operator-e2e",
		Neo4jEnabled:        false,
		Neo4jDatabase:       "neo4j",
	}
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
