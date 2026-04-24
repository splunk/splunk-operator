/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apps

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"gocloud.dev/blob"
	"gocloud.dev/blob/s3blob"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	appsv1alpha1 "github.com/splunk/splunk-operator/api/apps/v1alpha1"
)

// AppSourceReconciler reconciles a AppSource object
type AppSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.splunk.com,resources=appsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.splunk.com,resources=appsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.splunk.com,resources=appsources/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppSource object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *AppSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	appSourceInstance := &appsv1alpha1.AppSource{}

	if err := r.Get(ctx, req.NamespacedName, appSourceInstance); err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			logger.Info("AppSource resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get AppSource")
		return ctrl.Result{}, err
	}

	// initialize conditions if needed
	if len(appSourceInstance.Status.Conditions) == 0 {
		// initialize all the conditions
		for _, conditionType := range []string{
			appsv1alpha1.AppSourceConditionReady,
			appsv1alpha1.AppSourceConditionSecretValid,
			appsv1alpha1.AppSourceConditionBackendAccessible,
			appsv1alpha1.AppSourceConditionAppSynced,
		} {
			meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
				Type:    conditionType,
				Status:  metav1.ConditionUnknown,
				Reason:  "Initializing",
				Message: "Waiting for first reconciliation",
			})
		}
		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to initialize AppSource conditions")
			return ctrl.Result{}, err
		}

		// Requeue to process the AppSource after conditions are initialized
		return ctrl.Result{Requeue: true}, nil
	}

	// check if we need to reconcile based on the observed generation changes or the periodic poll
	needsToReconcile := appSourceInstance.Generation != appSourceInstance.Status.ObservedGeneration
	// if the generation changed, we need to reconcile asap; we shouldnt enter this block
	// if the generation has not changed, check if we need to reconcile based on the periodic poll
	if !needsToReconcile && appSourceInstance.Status.LastPolledTime != nil {
		// calculate the next poll time; we get the last sync time and add the poll interval
		nextPoll := appSourceInstance.Status.LastPolledTime.Add(time.Duration(*appSourceInstance.Spec.PollIntervalSeconds) * time.Second)
		// check if the current time is earlier than polling time
		if time.Now().Before(nextPoll) {
			return ctrl.Result{RequeueAfter: time.Until(nextPoll)}, nil
		}
	}

	// at this point we know it needs to reconcile
	logger.Info("Reconciling AppSource", "namespacedName", req.NamespacedName, "name", req.Name, "secretName", appSourceInstance.Spec.Auth.SecretRef.Name)
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      appSourceInstance.Spec.Auth.SecretRef.Name,
		Namespace: appSourceInstance.Namespace,
	}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		logger.Error(err, "Failed to get secret", "secretName", appSourceInstance.Spec.Auth.SecretRef.Name)
		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionSecretValid,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "SecretNotFound",
			Message:            "reference secret not found: " + appSourceInstance.Spec.Auth.SecretRef.Name,
		})
		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "SecretNotFound",
			Message:            "secret validation failed",
		})
		appSourceInstance.Status.ObservedGeneration = appSourceInstance.Generation
		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to update AppSource status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	// secrets are valid
	meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
		Type:               appsv1alpha1.AppSourceConditionSecretValid,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: appSourceInstance.Generation,
		Reason:             "SecretValid",
		Message:            "secret exists and accessible",
	})

	// Handle different backend types
	var discoveredApps []appsv1alpha1.DiscoveredApp
	var reconcileErr error

	switch appSourceInstance.Spec.Type {
	case "s3":
		discoveredApps, reconcileErr = r.reconcileS3(ctx, appSourceInstance, secret)
	case "splunkbase":
		discoveredApps, reconcileErr = r.reconcileSplunkBase(ctx, appSourceInstance, secret)
	default:
		reconcileErr = fmt.Errorf("unsupported backend type: %s", appSourceInstance.Spec.Type)
	}

	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}

	// Update status with discovered apps
	appSourceInstance.Status.DiscoveredApps = discoveredApps
	now := metav1.Now()
	appSourceInstance.Status.LastPolledTime = &now
	appSourceInstance.Status.ObservedGeneration = appSourceInstance.Generation

	meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
		Type:               appsv1alpha1.AppSourceConditionReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: appSourceInstance.Generation,
		Reason:             "Available",
		Message:            fmt.Sprintf("all checks passed, %d apps discovered", len(discoveredApps)),
	})

	if err := r.Status().Update(ctx, appSourceInstance); err != nil {
		logger.Error(err, "Failed to update AppSource status")
		return ctrl.Result{}, err
	}

	logger.Info("AppSource reconciled successfully", "discoveredApps", len(discoveredApps))

	// requeue for next poll
	requeueAfter := time.Duration(*appSourceInstance.Spec.PollIntervalSeconds) * time.Second
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *AppSourceReconciler) reconcileS3(ctx context.Context, appSourceInstance *appsv1alpha1.AppSource, secret *corev1.Secret) ([]appsv1alpha1.DiscoveredApp, error) {
	logger := logf.FromContext(ctx)

	// get bucket, region, and path
	bucket := appSourceInstance.Spec.S3.Bucket
	region := appSourceInstance.Spec.S3.Region
	path := appSourceInstance.Spec.S3.Path

	logger.Info("Bucket", "bucket", bucket)
	logger.Info("Region", "region", region)
	logger.Info("Path", "path", path)

	// Get AWS credentials from secret
	accessKey := string(secret.Data["s3_access_key"])
	secretAccessKey := string(secret.Data["s3_secret_key"])

	if accessKey == "" || secretAccessKey == "" {
		logger.Error(nil, "AWS credentials not found in secret")

		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionSecretValid,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "CredentialsMissing",
			Message:            "s3_access_key or s3_secret_key not found in secret",
		})
		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "CredentialsMissing",
			Message:            "AWS credentials missing from secret",
		})
		appSourceInstance.Status.ObservedGeneration = appSourceInstance.Generation
		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to update AppSource status")
			return nil, err
		}
		return nil, fmt.Errorf("AWS credentials (s3_access_key and s3_secret_key) not found in secret %s", appSourceInstance.Spec.Auth.SecretRef.Name)
	}

	bucketURL := fmt.Sprintf("s3://%s?region=%s", bucket, region)
	logger.Info("Bucket URL", "bucketURL", bucketURL)

	// set up s3 client via s3 sdk for authentication
	// doc: https://gocloud.dev/howto/blob/
	// doc: https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/config#LoadDefaultConfig
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey,       // access key
			secretAccessKey, // secret access key
			"",              // session token -> leave empty because we are using hardcoded credentials
		)),
	)
	if err != nil {
		logger.Error(err, "Failed to load AWS config")

		// update the backend accessible bc aws / sdk issue
		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionBackendAccessible,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "AWSConfigError",
			Message:            "failed to load AWS config: " + err.Error(), // might need to change this later, can check if we are leaking anything
		})
		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "AWSConfigError",
			Message:            "failed to configure AWS client",
		})
		appSourceInstance.Status.ObservedGeneration = appSourceInstance.Generation
		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to update AppSource status")
			return nil, err
		}

		return nil, err
	}

	awsClient := s3.NewFromConfig(cfg)
	bkt, err := s3blob.OpenBucket(ctx, awsClient, bucket, nil)
	if err != nil {
		logger.Error(err, "Failed to open bucket")

		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionBackendAccessible,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "BucketOpenFailed",
			Message:            "failed to open bucket: " + err.Error(),
		})
		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "BucketOpenFailed",
			Message:            "cannot access S3 bucket",
		})
		appSourceInstance.Status.ObservedGeneration = appSourceInstance.Generation
		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to update AppSource status")
			return nil, err
		}

		return nil, err
	}
	defer bkt.Close()

	// set backend accessible to true
	meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
		Type:               appsv1alpha1.AppSourceConditionBackendAccessible,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: appSourceInstance.Generation,
		Reason:             "BackendReachable",
		Message:            "S3 bucket is accessible",
	})

	// check if the prefix actually exists
	prefixIter := bkt.List(&blob.ListOptions{
		Prefix: path,
	})
	_, err = prefixIter.Next(ctx)
	if err == io.EOF { // prefix does not exist
		logger.Info("Prefix does not exist", "prefix", path)

		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionAppSynced,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "PrefixNotFound",
			Message:            "prefix does not exist",
		})

		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "PrefixNotFound",
			Message:            "prefix does not exist",
		})

		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to update AppSource status")
			return nil, err
		}

		return nil, nil
	} else if err != nil {
		logger.Error(err, "Failed to list objects")

		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionAppSynced,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "ListFailed",
			Message:            "Failed to list objects",
		})

		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:               appsv1alpha1.AppSourceConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: appSourceInstance.Generation,
			Reason:             "ListFailed",
			Message:            "Failed to list objects",
		})

		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to update AppSource status")
			return nil, err
		}

		return nil, nil
	}

	// list the apps in the bucket
	// create a list iterator
	// doc: https://pkg.go.dev/gocloud.dev/blob?utm_source=godoc#example-Bucket.List
	appsIter := bkt.List(&blob.ListOptions{
		Prefix: path, // shc-apps
	})

	// discoveredApps should store apps and its metadata (size, modified time, checksum/sha/etag/md5)
	discoveredApps := []appsv1alpha1.DiscoveredApp{}

	for {
		obj, err := appsIter.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error(err, "Failed to list objects")

			meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
				Type:               appsv1alpha1.AppSourceConditionAppSynced,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: appSourceInstance.Generation,
				Reason:             "ListFailed",
				Message:            "failed to list objects: " + err.Error(),
			})

			meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
				Type:               appsv1alpha1.AppSourceConditionReady,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: appSourceInstance.Generation,
				Reason:             "ListFailed",
				Message:            "failed to list apps from backend",
			})
			appSourceInstance.Status.ObservedGeneration = appSourceInstance.Generation
			if err := r.Status().Update(ctx, appSourceInstance); err != nil {
				logger.Error(err, "Failed to update AppSource status")
				return nil, err
			}

			return nil, err
		}
		// check if the object is .tgz, .spl, or .tar.gz
		if !strings.HasSuffix(obj.Key, ".tgz") && !strings.HasSuffix(obj.Key, ".spl") && !strings.HasSuffix(obj.Key, ".tar.gz") {
			continue
		}

		discoveredApps = append(discoveredApps, appsv1alpha1.DiscoveredApp{
			Name:         strings.Split(obj.Key, "/")[len(strings.Split(obj.Key, "/"))-1], // get only the package name
			Path:         obj.Key,                                                         // full path to the object
			Size:         int64(obj.Size),
			LastModified: metav1.NewTime(obj.ModTime),
			Checksum:     fmt.Sprintf("%x", obj.MD5),
		})

		// log the app metadata
		logger.Info("App metadata", "app", discoveredApps[len(discoveredApps)-1].Name, "path", discoveredApps[len(discoveredApps)-1].Path, "size", discoveredApps[len(discoveredApps)-1].Size, "modified", discoveredApps[len(discoveredApps)-1].LastModified, "checksum", discoveredApps[len(discoveredApps)-1].Checksum)
	}

	meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
		Type:               appsv1alpha1.AppSourceConditionAppSynced,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: appSourceInstance.Generation,
		Reason:             "AppsSynced",
		Message:            fmt.Sprintf("discovered %d apps", len(discoveredApps)),
	})

	return discoveredApps, nil
}

func (r *AppSourceReconciler) reconcileSplunkBase(ctx context.Context, appSourceInstance *appsv1alpha1.AppSource, secret *corev1.Secret) ([]appsv1alpha1.DiscoveredApp, error) {
	// logger := logf.FromContext(ctx)

	return nil, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.AppSource{}).
		Named("apps-appsource").
		Complete(r)
}
