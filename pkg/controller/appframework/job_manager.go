// Copyright (c) 2018-2024 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package appframework

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

// JobManager manages Kubernetes Jobs for app framework operations
type JobManager struct {
	Client       client.Client
	Scheme       *runtime.Scheme
	WorkerImage  string
	JobTemplates map[string]*JobTemplate
}

// JobTemplate defines a template for creating jobs
type JobTemplate struct {
	Name                  string
	BackoffLimit          int32
	ActiveDeadlineSeconds int64
	Resources             corev1.ResourceRequirements
	SecurityContext       *corev1.SecurityContext
	ServiceAccountName    string
}

// JobType represents different types of jobs
type JobType string

const (
	JobTypeRepositorySync JobType = "repository-sync"
	JobTypeAppDownload    JobType = "app-download"
	JobTypeAppInstall     JobType = "app-install"
	JobTypeAppUninstall   JobType = "app-uninstall"
	JobTypeAppValidation  JobType = "app-validation"
)

// NewJobManager creates a new JobManager
func NewJobManager(client client.Client, scheme *runtime.Scheme, workerImage string) *JobManager {
	jm := &JobManager{
		Client:       client,
		Scheme:       scheme,
		WorkerImage:  workerImage,
		JobTemplates: make(map[string]*JobTemplate),
	}

	// Initialize default job templates
	jm.initializeDefaultTemplates()
	return jm
}

// initializeDefaultTemplates sets up default job templates
func (jm *JobManager) initializeDefaultTemplates() {
	// Repository sync job template
	jm.JobTemplates[string(JobTypeRepositorySync)] = &JobTemplate{
		Name:                  "repository-sync",
		BackoffLimit:          3,
		ActiveDeadlineSeconds: 1800, // 30 minutes
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             &[]bool{true}[0],
			AllowPrivilegeEscalation: &[]bool{false}[0],
			ReadOnlyRootFilesystem:   &[]bool{true}[0],
		},
		ServiceAccountName: "app-framework-worker",
	}

	// App download job template
	jm.JobTemplates[string(JobTypeAppDownload)] = &JobTemplate{
		Name:                  "app-download",
		BackoffLimit:          3,
		ActiveDeadlineSeconds: 3600, // 1 hour
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             &[]bool{true}[0],
			AllowPrivilegeEscalation: &[]bool{false}[0],
			ReadOnlyRootFilesystem:   &[]bool{false}[0], // Need to write downloaded files
		},
		ServiceAccountName: "app-framework-worker",
	}

	// App install job template
	jm.JobTemplates[string(JobTypeAppInstall)] = &JobTemplate{
		Name:                  "app-install",
		BackoffLimit:          3,
		ActiveDeadlineSeconds: 1800, // 30 minutes
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             &[]bool{true}[0],
			AllowPrivilegeEscalation: &[]bool{false}[0],
			ReadOnlyRootFilesystem:   &[]bool{true}[0],
		},
		ServiceAccountName: "app-framework-installer",
	}

	// App uninstall job template
	jm.JobTemplates[string(JobTypeAppUninstall)] = &JobTemplate{
		Name:                  "app-uninstall",
		BackoffLimit:          3,
		ActiveDeadlineSeconds: 1800, // 30 minutes
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             &[]bool{true}[0],
			AllowPrivilegeEscalation: &[]bool{false}[0],
			ReadOnlyRootFilesystem:   &[]bool{true}[0],
		},
		ServiceAccountName: "app-framework-installer",
	}
}

// CreateRepositorySyncJob creates a job to sync a repository
func (jm *JobManager) CreateRepositorySyncJob(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) (*batchv1.Job, error) {
	logger := log.FromContext(ctx)

	jobName := fmt.Sprintf("repo-sync-%s-%d", repository.Name, time.Now().Unix())
	template := jm.JobTemplates[string(JobTypeRepositorySync)]

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: repository.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":             "app-framework",
				"app.kubernetes.io/component":        "repository-sync",
				"app.kubernetes.io/managed-by":       "app-framework-controller",
				"appframework.splunk.com/repository": repository.Name,
				"appframework.splunk.com/job-type":   string(JobTypeRepositorySync),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &template.BackoffLimit,
			ActiveDeadlineSeconds: &template.ActiveDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":             "app-framework",
						"app.kubernetes.io/component":        "repository-sync",
						"appframework.splunk.com/repository": repository.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: template.ServiceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						FSGroup:      &[]int64{1000}[0],
					},
					Containers: []corev1.Container{
						{
							Name:            "repository-sync",
							Image:           jm.WorkerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/app-framework-worker"},
							Args: []string{
								"sync-repository",
								"--repository=" + repository.Name,
								"--namespace=" + repository.Namespace,
							},
							Resources:       template.Resources,
							SecurityContext: template.SecurityContext,
							Env: []corev1.EnvVar{
								{
									Name:  "OPERATION_TYPE",
									Value: string(JobTypeRepositorySync),
								},
								{
									Name:  "REPOSITORY_NAME",
									Value: repository.Name,
								},
								{
									Name:  "REPOSITORY_NAMESPACE",
									Value: repository.Namespace,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "app-cache",
									MountPath: "/app-cache",
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "app-cache",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "app-framework-cache",
								},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Add credentials volume if secret is specified
	if repository.Spec.SecretRef != "" {
		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			job.Spec.Template.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{
				Name:      "credentials",
				MountPath: "/credentials",
				ReadOnly:  true,
			},
		)
		job.Spec.Template.Spec.Volumes = append(
			job.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: "credentials",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: repository.Spec.SecretRef,
					},
				},
			},
		)
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(repository, job, jm.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the job
	if err := jm.Client.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	logger.Info("Created repository sync job", "job", job.Name, "repository", repository.Name)
	return job, nil
}

// CreateAppDownloadJob creates a job to download an app
func (jm *JobManager) CreateAppDownloadJob(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, repository *appframeworkv1.AppFrameworkRepository) (*batchv1.Job, error) {
	logger := log.FromContext(ctx)

	jobName := fmt.Sprintf("download-%s-%d", deployment.Spec.AppName, time.Now().Unix())
	template := jm.JobTemplates[string(JobTypeAppDownload)]

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: deployment.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":             "app-framework",
				"app.kubernetes.io/component":        "app-download",
				"app.kubernetes.io/managed-by":       "app-framework-controller",
				"appframework.splunk.com/deployment": deployment.Name,
				"appframework.splunk.com/app":        deployment.Spec.AppName,
				"appframework.splunk.com/job-type":   string(JobTypeAppDownload),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &template.BackoffLimit,
			ActiveDeadlineSeconds: &template.ActiveDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":             "app-framework",
						"app.kubernetes.io/component":        "app-download",
						"appframework.splunk.com/deployment": deployment.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: template.ServiceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						FSGroup:      &[]int64{1000}[0],
					},
					Containers: []corev1.Container{
						{
							Name:            "app-download",
							Image:           jm.WorkerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/app-framework-worker"},
							Args: []string{
								"download-app",
								"--app=" + deployment.Spec.AppName,
								"--source=" + deployment.Spec.AppSource,
								"--repository=" + repository.Name,
								"--namespace=" + deployment.Namespace,
							},
							Resources:       template.Resources,
							SecurityContext: template.SecurityContext,
							Env: []corev1.EnvVar{
								{
									Name:  "OPERATION_TYPE",
									Value: string(JobTypeAppDownload),
								},
								{
									Name:  "APP_NAME",
									Value: deployment.Spec.AppName,
								},
								{
									Name:  "APP_SOURCE",
									Value: deployment.Spec.AppSource,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "app-cache",
									MountPath: "/app-cache",
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "app-cache",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "app-framework-cache",
								},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Add credentials volume if needed
	if repository.Spec.SecretRef != "" {
		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			job.Spec.Template.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{
				Name:      "credentials",
				MountPath: "/credentials",
				ReadOnly:  true,
			},
		)
		job.Spec.Template.Spec.Volumes = append(
			job.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: "credentials",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: repository.Spec.SecretRef,
					},
				},
			},
		)
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(deployment, job, jm.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the job
	if err := jm.Client.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	logger.Info("Created app download job", "job", job.Name, "app", deployment.Spec.AppName)
	return job, nil
}

// CreateAppInstallJob creates a job to install an app
func (jm *JobManager) CreateAppInstallJob(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (*batchv1.Job, error) {
	logger := log.FromContext(ctx)

	jobName := fmt.Sprintf("install-%s-%d", deployment.Spec.AppName, time.Now().Unix())
	template := jm.JobTemplates[string(JobTypeAppInstall)]

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: deployment.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":             "app-framework",
				"app.kubernetes.io/component":        "app-install",
				"app.kubernetes.io/managed-by":       "app-framework-controller",
				"appframework.splunk.com/deployment": deployment.Name,
				"appframework.splunk.com/app":        deployment.Spec.AppName,
				"appframework.splunk.com/job-type":   string(JobTypeAppInstall),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &template.BackoffLimit,
			ActiveDeadlineSeconds: &template.ActiveDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":             "app-framework",
						"app.kubernetes.io/component":        "app-install",
						"appframework.splunk.com/deployment": deployment.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: template.ServiceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						FSGroup:      &[]int64{1000}[0],
					},
					Containers: []corev1.Container{
						{
							Name:            "app-install",
							Image:           jm.WorkerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/app-framework-worker"},
							Args: []string{
								"install-app",
								"--app=" + deployment.Spec.AppName,
								"--scope=" + deployment.Spec.Scope,
								"--target-pods=" + fmt.Sprintf("%v", deployment.Spec.TargetPods),
								"--namespace=" + deployment.Namespace,
							},
							Resources:       template.Resources,
							SecurityContext: template.SecurityContext,
							Env: []corev1.EnvVar{
								{
									Name:  "OPERATION_TYPE",
									Value: string(JobTypeAppInstall),
								},
								{
									Name:  "APP_NAME",
									Value: deployment.Spec.AppName,
								},
								{
									Name:  "TARGET_NAMESPACE",
									Value: deployment.Namespace,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "app-cache",
									MountPath: "/app-cache",
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "app-cache",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "app-framework-cache",
								},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(deployment, job, jm.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the job
	if err := jm.Client.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	logger.Info("Created app install job", "job", job.Name, "app", deployment.Spec.AppName)
	return job, nil
}

// CreateAppUninstallJob creates a job to uninstall an app
func (jm *JobManager) CreateAppUninstallJob(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (*batchv1.Job, error) {
	logger := log.FromContext(ctx)

	jobName := fmt.Sprintf("uninstall-%s-%d", deployment.Spec.AppName, time.Now().Unix())
	template := jm.JobTemplates[string(JobTypeAppUninstall)]

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: deployment.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":             "app-framework",
				"app.kubernetes.io/component":        "app-uninstall",
				"app.kubernetes.io/managed-by":       "app-framework-controller",
				"appframework.splunk.com/deployment": deployment.Name,
				"appframework.splunk.com/app":        deployment.Spec.AppName,
				"appframework.splunk.com/job-type":   string(JobTypeAppUninstall),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &template.BackoffLimit,
			ActiveDeadlineSeconds: &template.ActiveDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":             "app-framework",
						"app.kubernetes.io/component":        "app-uninstall",
						"appframework.splunk.com/deployment": deployment.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: template.ServiceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						FSGroup:      &[]int64{1000}[0],
					},
					Containers: []corev1.Container{
						{
							Name:            "app-uninstall",
							Image:           jm.WorkerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/app-framework-worker"},
							Args: []string{
								"uninstall-app",
								"--app=" + deployment.Spec.AppName,
								"--scope=" + deployment.Spec.Scope,
								"--target-pods=" + fmt.Sprintf("%v", deployment.Spec.TargetPods),
								"--namespace=" + deployment.Namespace,
							},
							Resources:       template.Resources,
							SecurityContext: template.SecurityContext,
							Env: []corev1.EnvVar{
								{
									Name:  "OPERATION_TYPE",
									Value: string(JobTypeAppUninstall),
								},
								{
									Name:  "APP_NAME",
									Value: deployment.Spec.AppName,
								},
								{
									Name:  "TARGET_NAMESPACE",
									Value: deployment.Namespace,
								},
								{
									Name:  "DELETION_POLICY",
									Value: getStringValue(deployment.Spec.DeletionPolicy.Strategy, "graceful"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "app-cache",
									MountPath: "/app-cache",
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "app-cache",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "app-framework-cache",
								},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(deployment, job, jm.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the job
	if err := jm.Client.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	logger.Info("Created app uninstall job", "job", job.Name, "app", deployment.Spec.AppName)
	return job, nil
}

// GetActiveJob gets an active job for a repository and job type
func (jm *JobManager) GetActiveJob(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository, jobType string) (*batchv1.Job, error) {
	jobList := &batchv1.JobList{}

	labelSelector := labels.SelectorFromSet(labels.Set{
		"appframework.splunk.com/repository": repository.Name,
		"appframework.splunk.com/job-type":   jobType,
	})

	if err := jm.Client.List(ctx, jobList, &client.ListOptions{
		Namespace:     repository.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	// Look for active jobs (not completed or failed)
	for _, job := range jobList.Items {
		if job.Status.CompletionTime == nil && job.Status.Failed == 0 {
			return &job, nil
		}
	}

	return nil, nil
}

// CancelJobsForRepository cancels all jobs for a repository
func (jm *JobManager) CancelJobsForRepository(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) error {
	logger := log.FromContext(ctx)

	jobList := &batchv1.JobList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		"appframework.splunk.com/repository": repository.Name,
	})

	if err := jm.Client.List(ctx, jobList, &client.ListOptions{
		Namespace:     repository.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	for _, job := range jobList.Items {
		if job.Status.CompletionTime == nil {
			logger.Info("Cancelling job", "job", job.Name)
			if err := jm.Client.Delete(ctx, &job); err != nil {
				logger.Error(err, "Failed to delete job", "job", job.Name)
			}
		}
	}

	return nil
}

// getStringValue returns the string value or a default if empty
func getStringValue(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
