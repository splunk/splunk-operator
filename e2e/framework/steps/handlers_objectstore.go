package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/objectstore"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterObjectstoreHandlers registers object store helper steps.
func RegisterObjectstoreHandlers(reg *Registry) {
	reg.Register("objectstore.list", handleObjectstoreList)
	reg.Register("objectstore.upload", handleObjectstoreUpload)
	reg.Register("objectstore.upload.list", handleObjectstoreUploadList)
	reg.Register("objectstore.download", handleObjectstoreDownload)
	reg.Register("objectstore.download.list", handleObjectstoreDownloadList)
	reg.Register("objectstore.delete", handleObjectstoreDelete)
	reg.Register("objectstore.delete.list", handleObjectstoreDeleteList)
	reg.Register("objectstore.secret.ensure", handleObjectstoreSecretEnsure)
	reg.Register("assert.objectstore.prefix.exists", handleObjectstorePrefixExists)
}

func handleObjectstoreList(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	prefix := expandVars(getString(step.With, "prefix", ""), exec.Vars)
	objects, err := provider.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(objects))
	for _, obj := range objects {
		if obj.Key != "" {
			keys = append(keys, obj.Key)
		}
	}
	joined := strings.Join(keys, ",")
	exec.Vars["last_objectstore_keys"] = joined
	exec.Vars["last_objectstore_count"] = fmt.Sprintf("%d", len(keys))
	if len(keys) > 0 {
		exec.Vars["last_objectstore_key"] = keys[0]
	}
	return map[string]string{
		"count": exec.Vars["last_objectstore_count"],
		"keys":  joined,
	}, nil
}

func handleObjectstoreUpload(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	localPath := expandVars(getString(step.With, "local_path", ""), exec.Vars)
	if localPath == "" {
		return nil, fmt.Errorf("local_path is required")
	}
	key := expandVars(getString(step.With, "key", ""), exec.Vars)
	if key == "" {
		key = filepath.Base(localPath)
	}

	info, err := provider.Upload(ctx, key, localPath)
	if err != nil {
		return nil, err
	}
	exec.Vars["last_objectstore_key"] = info.Key
	exec.Vars["last_objectstore_size"] = fmt.Sprintf("%d", info.Size)
	return map[string]string{
		"key":  info.Key,
		"size": fmt.Sprintf("%d", info.Size),
	}, nil
}

func handleObjectstoreUploadList(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	files, err := getStringList(step.With, "files")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("files are required")
	}
	localDir := expandVars(getString(step.With, "local_dir", ""), exec.Vars)
	keyPrefix := expandVars(getString(step.With, "key_prefix", ""), exec.Vars)
	uploaded := make([]string, 0, len(files))
	for _, fileName := range files {
		localPath := fileName
		if localDir != "" {
			localPath = filepath.Join(localDir, fileName)
		}
		key := fileName
		if keyPrefix != "" {
			key = filepath.ToSlash(filepath.Join(keyPrefix, fileName))
		}
		info, err := provider.Upload(ctx, key, localPath)
		if err != nil {
			return nil, err
		}
		uploaded = append(uploaded, info.Key)
	}
	exec.Vars["last_objectstore_keys"] = strings.Join(uploaded, ",")
	exec.Vars["last_objectstore_count"] = fmt.Sprintf("%d", len(uploaded))
	if len(uploaded) > 0 {
		exec.Vars["last_objectstore_key"] = uploaded[0]
	}
	return map[string]string{"count": exec.Vars["last_objectstore_count"], "keys": exec.Vars["last_objectstore_keys"]}, nil
}

func handleObjectstoreDownload(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	key := expandVars(getString(step.With, "key", ""), exec.Vars)
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}
	localPath := expandVars(getString(step.With, "local_path", ""), exec.Vars)
	if localPath == "" {
		localPath = filepath.Join(exec.Artifacts.RunDir, fmt.Sprintf("objectstore-%s", sanitize(key)))
	}

	info, err := provider.Download(ctx, key, localPath)
	if err != nil {
		return nil, err
	}
	exec.Vars["last_objectstore_key"] = info.Key
	exec.Vars["last_objectstore_path"] = localPath
	exec.Vars["last_objectstore_size"] = fmt.Sprintf("%d", info.Size)
	metadata := map[string]string{
		"key":  info.Key,
		"path": localPath,
		"size": fmt.Sprintf("%d", info.Size),
	}
	if !info.LastModified.IsZero() {
		metadata["last_modified"] = info.LastModified.UTC().Format("2006-01-02T15:04:05Z")
	}
	return metadata, nil
}

func handleObjectstoreDownloadList(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	keys, err := getStringList(step.With, "keys")
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("keys are required")
	}
	localDir := expandVars(getString(step.With, "local_dir", ""), exec.Vars)
	if localDir == "" {
		localDir = filepath.Join(exec.Artifacts.RunDir, "objectstore")
	}
	downloaded := make([]string, 0, len(keys))
	for _, key := range keys {
		localPath := filepath.Join(localDir, filepath.Base(key))
		info, err := provider.Download(ctx, key, localPath)
		if err != nil {
			return nil, err
		}
		downloaded = append(downloaded, info.Key)
	}
	exec.Vars["last_objectstore_keys"] = strings.Join(downloaded, ",")
	exec.Vars["last_objectstore_count"] = fmt.Sprintf("%d", len(downloaded))
	return map[string]string{"count": exec.Vars["last_objectstore_count"], "keys": exec.Vars["last_objectstore_keys"]}, nil
}

func handleObjectstoreDelete(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	key := expandVars(getString(step.With, "key", ""), exec.Vars)
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if err := provider.Delete(ctx, key); err != nil {
		return nil, err
	}
	return map[string]string{"key": key}, nil
}

func handleObjectstoreDeleteList(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	keys, err := getStringList(step.With, "keys")
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("keys are required")
	}
	for _, key := range keys {
		if err := provider.Delete(ctx, key); err != nil {
			return nil, err
		}
	}
	exec.Vars["last_objectstore_keys"] = strings.Join(keys, ",")
	exec.Vars["last_objectstore_count"] = fmt.Sprintf("%d", len(keys))
	return map[string]string{"count": exec.Vars["last_objectstore_count"]}, nil
}

func handleObjectstorePrefixExists(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	provider, err := objectstoreProvider(ctx, exec, step.With)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	prefix := expandVars(getString(step.With, "prefix", ""), exec.Vars)
	if prefix == "" {
		return nil, fmt.Errorf("prefix is required")
	}
	expected := getBool(step.With, "exists", true)
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	interval := 5 * time.Second
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		objects, err := provider.List(ctx, prefix)
		if err != nil {
			return nil, err
		}
		found := len(objects) > 0
		if found == expected {
			return map[string]string{"prefix": prefix, "count": fmt.Sprintf("%d", len(objects))}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("objectstore prefix %s existence did not reach expected state within %s", prefix, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func handleObjectstoreSecretEnsure(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	secretName := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if secretName == "" {
		secretName = fmt.Sprintf("splunk-s3-index-%s", namespace)
	}

	cfg := baseObjectstoreConfig(exec)
	providerRaw := getStringWithVars(step.With, "provider", cfg.Provider, exec.Vars)
	provider := objectstore.NormalizeProvider(providerRaw)
	if provider == "" {
		return nil, fmt.Errorf("objectstore provider is required")
	}

	data, err := buildObjectstoreSecretData(provider, cfg, step.With, exec)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: namespace, Name: secretName}
	err = exec.Kube.Client.Get(ctx, key, secret)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}
		if err := exec.Kube.Client.Create(ctx, secret); err != nil {
			return nil, err
		}
	} else {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte, len(data))
		}
		for key, value := range data {
			secret.Data[key] = value
		}
		secret.Type = corev1.SecretTypeOpaque
		if err := exec.Kube.Client.Update(ctx, secret); err != nil {
			return nil, err
		}
	}

	exec.Vars["objectstore_secret_name"] = secretName
	return map[string]string{"name": secretName, "namespace": namespace, "provider": provider}, nil
}

func buildObjectstoreSecretData(provider string, cfg objectstore.Config, params map[string]interface{}, exec *Context) (map[string][]byte, error) {
	switch provider {
	case "s3":
		accessKey := getStringWithVars(params, "access_key", cfg.AccessKey, exec.Vars)
		secretKey := getStringWithVars(params, "secret_key", cfg.SecretKey, exec.Vars)
		if accessKey == "" || secretKey == "" {
			return nil, fmt.Errorf("s3 access_key and secret_key are required")
		}
		return map[string][]byte{
			"s3_access_key": []byte(accessKey),
			"s3_secret_key": []byte(secretKey),
		}, nil
	case "gcs":
		creds := getStringWithVars(params, "gcp_credentials_json", cfg.GCPCredentialsJSON, exec.Vars)
		if creds == "" && cfg.GCPCredentialsFile != "" {
			payload, err := os.ReadFile(cfg.GCPCredentialsFile)
			if err != nil {
				return nil, err
			}
			creds = string(payload)
		}
		if creds == "" {
			return nil, fmt.Errorf("gcp credentials JSON is required")
		}
		return map[string][]byte{"key.json": []byte(creds)}, nil
	case "azure":
		account := getStringWithVars(params, "azure_account", cfg.AzureAccount, exec.Vars)
		key := getStringWithVars(params, "azure_key", cfg.AzureKey, exec.Vars)
		if account == "" || key == "" {
			return nil, fmt.Errorf("azure account and key are required")
		}
		return map[string][]byte{
			"azure_sa_name":       []byte(account),
			"azure_sa_secret_key": []byte(key),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported objectstore provider: %s", provider)
	}
}

func objectstoreProvider(ctx context.Context, exec *Context, params map[string]interface{}) (objectstore.Provider, error) {
	base := baseObjectstoreConfig(exec)
	cfg := objectstore.Config{
		Provider:           getStringWithVars(params, "provider", base.Provider, exec.Vars),
		Bucket:             getStringWithVars(params, "bucket", base.Bucket, exec.Vars),
		Prefix:             getStringWithVars(params, "base_prefix", base.Prefix, exec.Vars),
		Region:             getStringWithVars(params, "region", base.Region, exec.Vars),
		Endpoint:           getStringWithVars(params, "endpoint", base.Endpoint, exec.Vars),
		AccessKey:          getStringWithVars(params, "access_key", base.AccessKey, exec.Vars),
		SecretKey:          getStringWithVars(params, "secret_key", base.SecretKey, exec.Vars),
		SessionToken:       getStringWithVars(params, "session_token", base.SessionToken, exec.Vars),
		S3PathStyle:        getBool(params, "s3_path_style", base.S3PathStyle),
		GCPProject:         getStringWithVars(params, "gcp_project", base.GCPProject, exec.Vars),
		GCPCredentialsFile: getStringWithVars(params, "gcp_credentials_file", base.GCPCredentialsFile, exec.Vars),
		GCPCredentialsJSON: getStringWithVars(params, "gcp_credentials_json", base.GCPCredentialsJSON, exec.Vars),
		AzureAccount:       getStringWithVars(params, "azure_account", base.AzureAccount, exec.Vars),
		AzureKey:           getStringWithVars(params, "azure_key", base.AzureKey, exec.Vars),
		AzureEndpoint:      getStringWithVars(params, "azure_endpoint", base.AzureEndpoint, exec.Vars),
		AzureSASToken:      getStringWithVars(params, "azure_sas_token", base.AzureSASToken, exec.Vars),
	}
	return objectstore.NewProvider(ctx, cfg)
}

func getStringWithVars(params map[string]interface{}, key string, fallback string, vars map[string]string) string {
	value := getString(params, key, "")
	if value == "" {
		return fallback
	}
	return expandVars(value, vars)
}

func baseObjectstoreConfig(exec *Context) objectstore.Config {
	if exec == nil || exec.Config == nil {
		return objectstore.Config{}
	}
	return objectstore.Config{
		Provider:           exec.Config.ObjectStoreProvider,
		Bucket:             exec.Config.ObjectStoreBucket,
		Prefix:             exec.Config.ObjectStorePrefix,
		Region:             exec.Config.ObjectStoreRegion,
		Endpoint:           exec.Config.ObjectStoreEndpoint,
		AccessKey:          exec.Config.ObjectStoreAccessKey,
		SecretKey:          exec.Config.ObjectStoreSecretKey,
		SessionToken:       exec.Config.ObjectStoreSessionToken,
		S3PathStyle:        exec.Config.ObjectStoreS3PathStyle,
		GCPProject:         exec.Config.ObjectStoreGCPProject,
		GCPCredentialsFile: exec.Config.ObjectStoreGCPCredentialsFile,
		GCPCredentialsJSON: exec.Config.ObjectStoreGCPCredentialsJSON,
		AzureAccount:       exec.Config.ObjectStoreAzureAccount,
		AzureKey:           exec.Config.ObjectStoreAzureKey,
		AzureEndpoint:      exec.Config.ObjectStoreAzureEndpoint,
		AzureSASToken:      exec.Config.ObjectStoreAzureSASToken,
	}
}
