package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileConfig represents the structured YAML configuration.
type FileConfig struct {
	Run         *RunFileConfig         `yaml:"run"`
	Kube        *KubeFileConfig        `yaml:"kube"`
	Operator    *OperatorFileConfig    `yaml:"operator"`
	Splunk      *SplunkFileConfig      `yaml:"splunk"`
	Logging     *LoggingFileConfig     `yaml:"logging"`
	Metrics     *MetricsFileConfig     `yaml:"metrics"`
	Graph       *GraphFileConfig       `yaml:"graph"`
	Objectstore *ObjectstoreFileConfig `yaml:"objectstore"`
	OTel        *OTelFileConfig        `yaml:"otel"`
	Neo4j       *Neo4jFileConfig       `yaml:"neo4j"`
}

type RunFileConfig struct {
	ID               *string     `yaml:"id"`
	SpecDir          *string     `yaml:"spec_dir"`
	DatasetRegistry  *string     `yaml:"dataset_registry"`
	ArtifactDir      *string     `yaml:"artifact_dir"`
	IncludeTags      *StringList `yaml:"include_tags"`
	ExcludeTags      *StringList `yaml:"exclude_tags"`
	Capabilities     *StringList `yaml:"capabilities"`
	Parallel         *int        `yaml:"parallel"`
	TopologyMode     *string     `yaml:"topology_mode"`
	DefaultTimeout   *string     `yaml:"default_timeout"`
	ProgressInterval *string     `yaml:"progress_interval"`
	SkipTeardown     *bool       `yaml:"skip_teardown"`
	LogCollection    *string     `yaml:"log_collection"`
}

type KubeFileConfig struct {
	Kubeconfig      *string `yaml:"kubeconfig"`
	NamespacePrefix *string `yaml:"namespace_prefix"`
	ClusterProvider *string `yaml:"cluster_provider"`
	ClusterWide     *bool   `yaml:"cluster_wide"`
}

type OperatorFileConfig struct {
	Image      *string `yaml:"image"`
	Namespace  *string `yaml:"namespace"`
	Deployment *string `yaml:"deployment"`
}

type SplunkFileConfig struct {
	Image   *string            `yaml:"image"`
	LogTail *int               `yaml:"log_tail"`
	Splunkd *SplunkdFileConfig `yaml:"splunkd"`
}

type SplunkdFileConfig struct {
	Endpoint *string `yaml:"endpoint"`
	Username *string `yaml:"username"`
	Password *string `yaml:"password"`
	Insecure *bool   `yaml:"insecure"`
	MgmtPort *int    `yaml:"mgmt_port"`
	HECPort  *int    `yaml:"hec_port"`
}

type LoggingFileConfig struct {
	Format *string `yaml:"format"`
	Level  *string `yaml:"level"`
}

type MetricsFileConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Path    *string `yaml:"path"`
}

type GraphFileConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type ObjectstoreFileConfig struct {
	Provider           *string `yaml:"provider"`
	Bucket             *string `yaml:"bucket"`
	Prefix             *string `yaml:"prefix"`
	Region             *string `yaml:"region"`
	Endpoint           *string `yaml:"endpoint"`
	AccessKey          *string `yaml:"access_key"`
	SecretKey          *string `yaml:"secret_key"`
	SessionToken       *string `yaml:"session_token"`
	S3PathStyle        *bool   `yaml:"s3_path_style"`
	GCPProject         *string `yaml:"gcp_project"`
	GCPCredentialsFile *string `yaml:"gcp_credentials_file"`
	GCPCredentialsJSON *string `yaml:"gcp_credentials_json"`
	AzureAccount       *string `yaml:"azure_account"`
	AzureKey           *string `yaml:"azure_key"`
	AzureEndpoint      *string `yaml:"azure_endpoint"`
	AzureSASToken      *string `yaml:"azure_sas_token"`
}

type OTelFileConfig struct {
	Enabled       *bool   `yaml:"enabled"`
	Endpoint      *string `yaml:"endpoint"`
	Headers       *string `yaml:"headers"`
	Insecure      *bool   `yaml:"insecure"`
	ServiceName   *string `yaml:"service_name"`
	ResourceAttrs *string `yaml:"resource_attrs"`
}

type Neo4jFileConfig struct {
	Enabled  *bool   `yaml:"enabled"`
	URI      *string `yaml:"uri"`
	User     *string `yaml:"user"`
	Password *string `yaml:"password"`
	Database *string `yaml:"database"`
}

// StringList supports string or list YAML values.
type StringList []string

func (s *StringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*s = splitCSV(value.Value)
		return nil
	case yaml.SequenceNode:
		out := make([]string, 0, len(value.Content))
		for _, node := range value.Content {
			if node.Kind != yaml.ScalarNode {
				return fmt.Errorf("string list must contain only scalars")
			}
			item := strings.TrimSpace(node.Value)
			if item != "" {
				out = append(out, item)
			}
		}
		*s = out
		return nil
	default:
		return fmt.Errorf("string list must be a string or list")
	}
}

func detectConfigPath(args []string, envValue string) string {
	path := strings.TrimSpace(envValue)
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "-config" || arg == "--config" {
			if i+1 < len(args) {
				path = strings.TrimSpace(args[i+1])
			}
			continue
		}
		if strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "--config=") {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) == 2 {
				path = strings.TrimSpace(parts[1])
			}
		}
	}
	return strings.TrimSpace(path)
}

func loadFileConfig(path string) (*FileConfig, error) {
	expanded := expandPath(path)
	if expanded == "" {
		return nil, nil
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, err
	}
	var cfg FileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyFileConfig(cfg *Config, fileCfg *FileConfig) error {
	if cfg == nil || fileCfg == nil {
		return nil
	}
	if fileCfg.Run != nil {
		run := fileCfg.Run
		if run.ID != nil {
			cfg.RunID = strings.TrimSpace(*run.ID)
		}
		if run.SpecDir != nil {
			cfg.SpecDir = expandPath(*run.SpecDir)
		}
		if run.DatasetRegistry != nil {
			cfg.DatasetRegistry = expandPath(*run.DatasetRegistry)
		}
		if run.ArtifactDir != nil {
			cfg.ArtifactDir = expandPath(*run.ArtifactDir)
		}
		if run.IncludeTags != nil {
			cfg.IncludeTags = append([]string(nil), (*run.IncludeTags)...)
		}
		if run.ExcludeTags != nil {
			cfg.ExcludeTags = append([]string(nil), (*run.ExcludeTags)...)
		}
		if run.Capabilities != nil {
			cfg.Capabilities = append([]string(nil), (*run.Capabilities)...)
		}
		if run.Parallel != nil {
			cfg.Parallelism = *run.Parallel
		}
		if run.TopologyMode != nil {
			cfg.TopologyMode = strings.TrimSpace(*run.TopologyMode)
		}
		if run.DefaultTimeout != nil {
			duration, err := time.ParseDuration(strings.TrimSpace(*run.DefaultTimeout))
			if err != nil {
				return fmt.Errorf("invalid run.default_timeout: %w", err)
			}
			cfg.DefaultTimeout = duration
		}
		if run.ProgressInterval != nil {
			duration, err := time.ParseDuration(strings.TrimSpace(*run.ProgressInterval))
			if err != nil {
				return fmt.Errorf("invalid run.progress_interval: %w", err)
			}
			cfg.ProgressInterval = duration
		}
		if run.SkipTeardown != nil {
			cfg.SkipTeardown = *run.SkipTeardown
		}
		if run.LogCollection != nil {
			cfg.LogCollection = strings.TrimSpace(*run.LogCollection)
		}
	}
	if fileCfg.Kube != nil {
		kube := fileCfg.Kube
		if kube.Kubeconfig != nil {
			cfg.Kubeconfig = expandPath(*kube.Kubeconfig)
		}
		if kube.NamespacePrefix != nil {
			cfg.NamespacePrefix = strings.TrimSpace(*kube.NamespacePrefix)
		}
		if kube.ClusterProvider != nil {
			cfg.ClusterProvider = strings.TrimSpace(*kube.ClusterProvider)
		}
		if kube.ClusterWide != nil {
			cfg.ClusterWide = *kube.ClusterWide
		}
	}
	if fileCfg.Operator != nil {
		op := fileCfg.Operator
		if op.Image != nil {
			cfg.OperatorImage = strings.TrimSpace(*op.Image)
		}
		if op.Namespace != nil {
			cfg.OperatorNamespace = strings.TrimSpace(*op.Namespace)
		}
		if op.Deployment != nil {
			cfg.OperatorDeployment = strings.TrimSpace(*op.Deployment)
		}
	}
	if fileCfg.Splunk != nil {
		splunk := fileCfg.Splunk
		if splunk.Image != nil {
			cfg.SplunkImage = strings.TrimSpace(*splunk.Image)
		}
		if splunk.LogTail != nil {
			cfg.SplunkLogTail = *splunk.LogTail
		}
		if splunk.Splunkd != nil {
			splunkd := splunk.Splunkd
			if splunkd.Endpoint != nil {
				cfg.SplunkdEndpoint = strings.TrimSpace(*splunkd.Endpoint)
			}
			if splunkd.Username != nil {
				cfg.SplunkdUsername = strings.TrimSpace(*splunkd.Username)
			}
			if splunkd.Password != nil {
				cfg.SplunkdPassword = strings.TrimSpace(*splunkd.Password)
			}
			if splunkd.Insecure != nil {
				cfg.SplunkdInsecure = *splunkd.Insecure
			}
			if splunkd.MgmtPort != nil {
				cfg.SplunkdMgmtPort = *splunkd.MgmtPort
			}
			if splunkd.HECPort != nil {
				cfg.SplunkdHECPort = *splunkd.HECPort
			}
		}
	}
	if fileCfg.Logging != nil {
		logging := fileCfg.Logging
		if logging.Format != nil {
			cfg.LogFormat = strings.TrimSpace(*logging.Format)
		}
		if logging.Level != nil {
			cfg.LogLevel = strings.TrimSpace(*logging.Level)
		}
	}
	if fileCfg.Metrics != nil {
		metrics := fileCfg.Metrics
		if metrics.Enabled != nil {
			cfg.MetricsEnabled = *metrics.Enabled
		}
		if metrics.Path != nil {
			cfg.MetricsPath = expandPath(*metrics.Path)
		}
	}
	if fileCfg.Graph != nil {
		graph := fileCfg.Graph
		if graph.Enabled != nil {
			cfg.GraphEnabled = *graph.Enabled
		}
	}
	if fileCfg.Objectstore != nil {
		obj := fileCfg.Objectstore
		if obj.Provider != nil {
			cfg.ObjectStoreProvider = strings.TrimSpace(*obj.Provider)
		}
		if obj.Bucket != nil {
			cfg.ObjectStoreBucket = strings.TrimSpace(*obj.Bucket)
		}
		if obj.Prefix != nil {
			cfg.ObjectStorePrefix = strings.TrimSpace(*obj.Prefix)
		}
		if obj.Region != nil {
			cfg.ObjectStoreRegion = strings.TrimSpace(*obj.Region)
		}
		if obj.Endpoint != nil {
			cfg.ObjectStoreEndpoint = strings.TrimSpace(*obj.Endpoint)
		}
		if obj.AccessKey != nil {
			cfg.ObjectStoreAccessKey = strings.TrimSpace(*obj.AccessKey)
		}
		if obj.SecretKey != nil {
			cfg.ObjectStoreSecretKey = strings.TrimSpace(*obj.SecretKey)
		}
		if obj.SessionToken != nil {
			cfg.ObjectStoreSessionToken = strings.TrimSpace(*obj.SessionToken)
		}
		if obj.S3PathStyle != nil {
			cfg.ObjectStoreS3PathStyle = *obj.S3PathStyle
		}
		if obj.GCPProject != nil {
			cfg.ObjectStoreGCPProject = strings.TrimSpace(*obj.GCPProject)
		}
		if obj.GCPCredentialsFile != nil {
			cfg.ObjectStoreGCPCredentialsFile = expandPath(*obj.GCPCredentialsFile)
		}
		if obj.GCPCredentialsJSON != nil {
			cfg.ObjectStoreGCPCredentialsJSON = strings.TrimSpace(*obj.GCPCredentialsJSON)
		}
		if obj.AzureAccount != nil {
			cfg.ObjectStoreAzureAccount = strings.TrimSpace(*obj.AzureAccount)
		}
		if obj.AzureKey != nil {
			cfg.ObjectStoreAzureKey = strings.TrimSpace(*obj.AzureKey)
		}
		if obj.AzureEndpoint != nil {
			cfg.ObjectStoreAzureEndpoint = strings.TrimSpace(*obj.AzureEndpoint)
		}
		if obj.AzureSASToken != nil {
			cfg.ObjectStoreAzureSASToken = strings.TrimSpace(*obj.AzureSASToken)
		}
	}
	if fileCfg.OTel != nil {
		otel := fileCfg.OTel
		if otel.Enabled != nil {
			cfg.OTelEnabled = *otel.Enabled
		}
		if otel.Endpoint != nil {
			cfg.OTelEndpoint = strings.TrimSpace(*otel.Endpoint)
		}
		if otel.Headers != nil {
			cfg.OTelHeaders = strings.TrimSpace(*otel.Headers)
		}
		if otel.Insecure != nil {
			cfg.OTelInsecure = *otel.Insecure
		}
		if otel.ServiceName != nil {
			cfg.OTelServiceName = strings.TrimSpace(*otel.ServiceName)
		}
		if otel.ResourceAttrs != nil {
			cfg.OTelResourceAttrs = strings.TrimSpace(*otel.ResourceAttrs)
		}
	}
	if fileCfg.Neo4j != nil {
		neo4j := fileCfg.Neo4j
		if neo4j.Enabled != nil {
			cfg.Neo4jEnabled = *neo4j.Enabled
		}
		if neo4j.URI != nil {
			cfg.Neo4jURI = strings.TrimSpace(*neo4j.URI)
		}
		if neo4j.User != nil {
			cfg.Neo4jUser = strings.TrimSpace(*neo4j.User)
		}
		if neo4j.Password != nil {
			cfg.Neo4jPassword = strings.TrimSpace(*neo4j.Password)
		}
		if neo4j.Database != nil {
			cfg.Neo4jDatabase = strings.TrimSpace(*neo4j.Database)
		}
	}
	return nil
}

func expandPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return trimmed
	}
	expanded := os.ExpandEnv(trimmed)
	if strings.HasPrefix(expanded, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~"))
		}
	}
	return expanded
}
