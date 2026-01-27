package data

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/objectstore"
)

// Fetch ensures a dataset file is available locally and returns its path.
func Fetch(ctx context.Context, dataset Dataset, cacheDir string, baseCfg objectstore.Config) (string, error) {
	source := strings.ToLower(strings.TrimSpace(dataset.Source))
	if source == "" || source == "local" || source == "file" {
		return dataset.File, nil
	}

	var provider string
	switch source {
	case "objectstore", "auto":
		provider = objectstore.NormalizeProvider(baseCfg.Provider)
	default:
		provider = objectstore.NormalizeProvider(source)
	}
	if provider == "" {
		return "", fmt.Errorf("unsupported dataset source: %s", dataset.Source)
	}
	if dataset.File == "" {
		return "", fmt.Errorf("dataset file is required")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	fileName := filepath.Base(dataset.File)
	localPath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s", dataset.Name, fileName))
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	cfg := baseCfg
	cfg.Provider = provider
	cfg.Bucket = strings.TrimSpace(dataset.Bucket)
	if cfg.Bucket == "" {
		cfg.Bucket = getSetting(dataset.Settings, "objectstore_bucket", "bucket")
	}
	if cfg.Bucket == "" {
		cfg.Bucket = baseCfg.Bucket
	}
	if cfg.Bucket == "" {
		return "", fmt.Errorf("dataset bucket is required for %s source", source)
	}

	if value := getSetting(dataset.Settings, "objectstore_region", "region"); value != "" {
		cfg.Region = value
	}
	if value := getSetting(dataset.Settings, "objectstore_endpoint", "endpoint"); value != "" {
		cfg.Endpoint = value
	}
	if value := getSetting(dataset.Settings, "objectstore_access_key", "access_key"); value != "" {
		cfg.AccessKey = value
	}
	if value := getSetting(dataset.Settings, "objectstore_secret_key", "secret_key"); value != "" {
		cfg.SecretKey = value
	}
	if value := getSetting(dataset.Settings, "objectstore_session_token", "session_token"); value != "" {
		cfg.SessionToken = value
	}
	if value, ok := getSettingBool(dataset.Settings, "objectstore_s3_path_style", "s3_path_style"); ok {
		cfg.S3PathStyle = value
	}
	if value := getSetting(dataset.Settings, "objectstore_gcp_project", "gcp_project"); value != "" {
		cfg.GCPProject = value
	}
	if value := getSetting(dataset.Settings, "objectstore_gcp_credentials_file", "gcp_credentials_file"); value != "" {
		cfg.GCPCredentialsFile = value
	}
	if value := getSetting(dataset.Settings, "objectstore_gcp_credentials_json", "gcp_credentials_json"); value != "" {
		cfg.GCPCredentialsJSON = value
	}
	if value := getSetting(dataset.Settings, "objectstore_azure_account", "azure_account"); value != "" {
		cfg.AzureAccount = value
	}
	if value := getSetting(dataset.Settings, "objectstore_azure_key", "azure_key"); value != "" {
		cfg.AzureKey = value
	}
	if value := getSetting(dataset.Settings, "objectstore_azure_endpoint", "azure_endpoint"); value != "" {
		cfg.AzureEndpoint = value
	}
	if value := getSetting(dataset.Settings, "objectstore_azure_sas_token", "azure_sas_token"); value != "" {
		cfg.AzureSASToken = value
	}
	if value := getSetting(dataset.Settings, "objectstore_prefix", "prefix"); value != "" {
		cfg.Prefix = value
	}

	if provider == "s3" && strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = strings.TrimSpace(os.Getenv("S3_REGION"))
		if cfg.Region == "" {
			cfg.Region = strings.TrimSpace(os.Getenv("AWS_REGION"))
		}
		if cfg.Region == "" {
			cfg.Region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
		}
	}

	key := strings.TrimLeft(dataset.File, "/")
	if key == "" {
		return "", fmt.Errorf("dataset file is required")
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		normalizedKey := strings.TrimLeft(key, "/")
		if normalizedKey == prefix || strings.HasPrefix(normalizedKey, prefix+"/") {
			cfg.Prefix = ""
		}
	}

	providerClient, err := objectstore.NewProvider(ctx, cfg)
	if err != nil {
		return "", err
	}
	defer providerClient.Close()

	if _, err := providerClient.Download(ctx, key, localPath); err != nil {
		return "", err
	}
	return localPath, nil
}

func getSetting(settings map[string]string, keys ...string) string {
	if len(settings) == 0 {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(settings[key]); value != "" {
			return value
		}
	}
	return ""
}

func getSettingBool(settings map[string]string, keys ...string) (bool, bool) {
	raw := getSetting(settings, keys...)
	if raw == "" {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "y":
		return true, true
	case "false", "0", "no", "n":
		return false, true
	default:
		return false, false
	}
}
