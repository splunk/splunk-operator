package raybuilder

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/controllers/storage"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	syaml "sigs.k8s.io/yaml"
)

func (b *Builder) ReconcileServeConfigMap(ctx context.Context, p *enterpriseApi.AIPlatform) error {
	log := log.FromContext(ctx) // Define logger

	// 2️⃣ List actual artifacts in storage
	storCli, err := storage.NewStorageClient(b.Client, p.Namespace, p.Spec.AppsVolume)
	if err != nil {
		log.Error(err, "failed to create storage client")
		return err
	}

	// 2️⃣ List actual artifacts in storage
	artfCli, err := storage.NewStorageClient(b.Client, p.Namespace, p.Spec.ArtifactsVolume)
	if err != nil {
		log.Error(err, "failed to create storage client")
		return err
	}

	// 4️⃣ Load the user’s apps fragment
	appsCM := &corev1.ConfigMap{}
	if err := b.Client.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: p.Name + "-applications"}, appsCM); err != nil {
		log.Error(err, "failed to get applications configmap")
		return err
	}

	var config Config
	err = syaml.Unmarshal([]byte(appsCM.Data["applications.yaml"]), &config)
	if err != nil {
		log.Error(err, "failed to unmarshal applications.yaml")
		return fmt.Errorf("failed to unmarshal applications.yaml: %w", err)
	}

	// 2) Prepare to filter in-place
	version := os.Getenv("MODEL_VERSION")
	apps := config.RayService.Applications[:0] // reset length to 0, keep capacity

	// 3) Walk the original list and keep only the ones that exist
	for _, a := range config.RayService.Applications {
		// build the key your Exists method expects
		// e.g. "appName-modelVersion.zip"
		key := fmt.Sprintf("%s-%s.zip", a.Name, version)

		ok, err := storCli.Exists(ctx, key)
		if err != nil {
			return fmt.Errorf("checking existence of %s: %w", key, err)
		}
		if !ok {
			// skip it — ZIP not in th bucket/container
			continue
		}

		// keep it
		apps = append(apps, a)
	}

	// 4) Assign the filtered slice back
	config.RayService.Applications = apps

	// 6️⃣ Build dynamic entries
	for i := range config.RayService.Applications {
		app := &config.RayService.Applications[i]
		fileName := fmt.Sprintf("%s-%s.zip", app.Name, os.Getenv("MODEL_VERSION"))
		wd := storCli.BuildWorkingDir(fileName)
		appName := app.Name
		if app.ImportPath == "" {
			app.ImportPath = "main:create_serve_app"
		}
		if app.RoutePrefix == "" {
			app.RoutePrefix = "/" + strings.ToLower(appName)
		}
		if app.RuntimeEnv != nil {
			if app.RuntimeEnv.WorkingDir == "" {
				app.RuntimeEnv.WorkingDir = wd
			}
			if app.RuntimeEnv.EnvVars == nil {
				app.RuntimeEnv.EnvVars = map[string]string{}
			}
			app.RuntimeEnv.EnvVars["SERVICE_NAME"] = sanitizeMetricName(p.Name)
			app.RuntimeEnv.EnvVars["SERVICE_INTERNAL_NAME"] = sanitizeMetricName(p.Name)
			app.RuntimeEnv.EnvVars["USE_SYSTEM_PERMISSIONS"] = "true"
			app.RuntimeEnv.EnvVars["APPLICATION_NAME"] = appName
			app.RuntimeEnv.EnvVars["ARTIFACTS_PROVIDER"] = artfCli.GetProvider()
			app.RuntimeEnv.EnvVars["ARTIFACTS_BUCKET"] = artfCli.GetBucket()

		} else {
			app.RuntimeEnv = new(RuntimeEnv)
			app.RuntimeEnv.WorkingDir = wd
			app.RuntimeEnv.EnvVars = map[string]string{
				"SERVICE_NAME":           sanitizeMetricName(p.Name),
				"SERVICE_INTERNAL_NAME":  sanitizeMetricName(p.Name),
				"USE_SYSTEM_PERMISSIONS": "true",
				"APPLICATION_NAME":       appName,
				"ARTIFACTS_PROVIDER":     artfCli.GetProvider(),
				"ARTIFACTS_BUCKET":       artfCli.GetBucket(),
			}
		}
		if app.RuntimeEnv.Pip == nil {
			app.RuntimeEnv.Pip = []string{
				"azure-storage-blob",
				"google-cloud-storage",
			}
		}
		app.Name = toCamelCase(app.Name)
	}

	serveConfig := config.RayService
	serveConfig.HTTPOptions = HTTPOptions{
		KeepAliveTimeoutS: 10,
	}
	serveConfig.LoggingConfig = LoggingConfig{
		LogLevel:        "INFO",
		LogsDir:         "/tmp/ray/logs",
		Encoding:        "JSON",
		EnableAccessLog: true,
	}
	out, err := syaml.Marshal(serveConfig)
	if err != nil {
		log.Error(err, "failed to marshal serveConfig")
		return fmt.Errorf("failed to marshal serveConfig: %w", err)
	}

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      p.Name + "-serveconfig",
		Namespace: p.Namespace,
	},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, b.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data["serveconfig.yaml"] = string(out)
		return controllerutil.SetOwnerReference(p, cm, b.Scheme)
	})
	return err
}

func toCamelCase(s string) string {
	words := strings.Split(s, "_")
	result := ""
	for i, word := range words {
		if i == 0 {
			result += word
			continue
		}
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		result += string(runes)
	}
	return result
}

// metricNameRE matches valid Prometheus metric name characters
var metricNameRE = regexp.MustCompile(`[^A-Za-z0-9_:]`)

// sanitizeMetricName replaces all invalid chars (including “-”) with “_”
// and ensures the first character is a letter or underscore.
func sanitizeMetricName(s string) string {
	// 1) replace anything not [A-Za-z0-9_:] with “_”
	m := metricNameRE.ReplaceAllString(s, "_")

	// 2) make sure it starts with [A-Za-z_:]
	if len(m) == 0 || !((m[0] >= 'A' && m[0] <= 'Z') ||
		(m[0] >= 'a' && m[0] <= 'z') || m[0] == '_' || m[0] == ':') {
		m = "_" + m
	}
	return m
}
