package steps

import (
	"fmt"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/artifacts"
	"github.com/splunk/splunk-operator/e2e/framework/config"
	"github.com/splunk/splunk-operator/e2e/framework/data"
	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	"github.com/splunk/splunk-operator/e2e/framework/objectstore"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/splunkd"
	"go.uber.org/zap"
)

// Context holds shared state for step execution.
type Context struct {
	RunID           string
	TestName        string
	Logger          *zap.Logger
	Artifacts       *artifacts.Writer
	DatasetRegistry *data.Registry
	Config          *config.Config
	Kube            *k8s.Client
	Splunkd         *splunkd.Client
	Spec            *spec.TestSpec
	Vars            map[string]string
}

// NewContext creates a new execution context for a test.
func NewContext(runID string, testName string, logger *zap.Logger, writer *artifacts.Writer, registry *data.Registry, cfg *config.Config, kube *k8s.Client, spec *spec.TestSpec) *Context {
	vars := make(map[string]string)
	if cfg != nil {
		if cfg.SplunkImage != "" {
			vars["splunk_image"] = cfg.SplunkImage
		}
		if cfg.OperatorImage != "" {
			vars["operator_image"] = cfg.OperatorImage
		}
		if cfg.ClusterProvider != "" {
			vars["cluster_provider"] = cfg.ClusterProvider
		}
		if cfg.ObjectStoreBucket != "" {
			vars["objectstore_bucket"] = cfg.ObjectStoreBucket
		}
		if cfg.ObjectStorePrefix != "" {
			prefix := cfg.ObjectStorePrefix
			if !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
			vars["objectstore_prefix"] = prefix
		}
		if cfg.ObjectStoreRegion != "" {
			vars["objectstore_region"] = cfg.ObjectStoreRegion
		}
		if cfg.ObjectStoreEndpoint != "" {
			vars["objectstore_endpoint"] = cfg.ObjectStoreEndpoint
		}
		if cfg.ObjectStoreProvider != "" {
			rawProvider := strings.ToLower(strings.TrimSpace(cfg.ObjectStoreProvider))
			normalized := objectstore.NormalizeProvider(rawProvider)
			vars["objectstore_provider"] = normalized
			switch normalized {
			case "s3":
				vars["objectstore_storage_type"] = "s3"
				if rawProvider == "minio" {
					vars["objectstore_app_provider"] = "minio"
				} else {
					vars["objectstore_app_provider"] = "aws"
				}
				if vars["objectstore_endpoint"] == "" && cfg.ObjectStoreRegion != "" {
					vars["objectstore_endpoint"] = fmt.Sprintf("https://s3-%s.amazonaws.com", cfg.ObjectStoreRegion)
				}
			case "gcs":
				vars["objectstore_storage_type"] = "gcs"
				vars["objectstore_app_provider"] = "gcp"
				if vars["objectstore_endpoint"] == "" {
					vars["objectstore_endpoint"] = "https://storage.googleapis.com"
				}
			case "azure":
				vars["objectstore_storage_type"] = "blob"
				vars["objectstore_app_provider"] = "azure"
				if vars["objectstore_endpoint"] == "" && cfg.ObjectStoreAzureAccount != "" {
					vars["objectstore_endpoint"] = fmt.Sprintf("https://%s.blob.core.windows.net", cfg.ObjectStoreAzureAccount)
				}
			}
		}
	}
	return &Context{
		RunID:           runID,
		TestName:        testName,
		Logger:          logger,
		Artifacts:       writer,
		DatasetRegistry: registry,
		Config:          cfg,
		Kube:            kube,
		Spec:            spec,
		Vars:            vars,
	}
}
