package enterprise

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Reconcile function for the SplunkApp
func ApplySplunkApp(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.SplunkApp) (ctrl.Result, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplySearchHeadCluster")

	var err error
	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError
	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// Fetch the SplunkApp instance
	app := cr

	// Fetch Git repo credentials from Kubernetes secret
	credentials, err := getGitCredentials(app.Spec.SecretRef)
	if err != nil {
		scopedLog.Error(err, "Failed to get Git credentials")
		return ctrl.Result{}, err
	}

	// Clone or fetch updates from the Git repo
	repoPath := fmt.Sprintf("/tmp/%s", app.Name)
	commitID, err := fetchGitRepo(app.Spec.GitRepo, app.Spec.Branch, credentials, repoPath)
	if err != nil {
		scopedLog.Error(err, "Failed to fetch Git repository")
		return ctrl.Result{}, err
	}

	// Use SLIM to package the Splunk app
	appName := app.Name
	if err := packageSplunkApp(ctx, repoPath, appName); err != nil {
		scopedLog.Error(err, "Failed to package Splunk app")
		return ctrl.Result{}, err
	}

	// Upload to S3
	sess := createAWSSession(app.Spec.S3Region, app.Spec.IrsaRoleArn)
	if err := uploadToS3(sess, app.Spec.S3Bucket, appName, commitID); err != nil {
		scopedLog.Error(err, "Failed to upload to S3")
		return ctrl.Result{}, err
	}

	// Update the status of the custom resource
	app.Status.AppName = appName
	app.Status.Version = commitID
	app.Status.GitCommitID = commitID
	app.Status.Repo = app.Spec.GitRepo
	app.Status.Branch = app.Spec.Branch

	scopedLog.Info("Successfully reconciled SplunkApp", "app", appName, "version", commitID)
	return ctrl.Result{}, nil
}

// Helper function to get Git credentials from a secret
func getGitCredentials(secretRef string) (map[string]string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	secret, err := clientset.CoreV1().Secrets("default").Get(context.TODO(), secretRef, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"username": string(secret.Data["username"]),
		"password": string(secret.Data["password"]),
	}, nil
}

// Fetch the Git repository and return the latest commit ID
func fetchGitRepo(repoURL, branch string, credentials map[string]string, repoPath string) (string, error) {
	repo, err := git.PlainClone(repoPath, false, &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
		Auth: &http.BasicAuth{
			Username: credentials["username"],
			Password: credentials["password"],
		},
	})
	if err != nil {
		if err != git.ErrRepositoryAlreadyExists {
			return "", err
		}
		repo, err = git.PlainOpen(repoPath)
		if err != nil {
			return "", err
		}
	}

	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}

	// Pull latest changes
	err = w.Pull(&git.PullOptions{
		RemoteName: "origin",
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branch)),
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return "", err
	}

	ref, err := repo.Head()
	if err != nil {
		return "", err
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return "", err
	}

	return commit.Hash.String(), nil
}

// Use SLIM framework to package the Splunk app
func packageSplunkApp(ctx context.Context, repoPath, appName string) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("packageSplunkApp")

	cmd := exec.Command("slim", "package", "--app-name", appName, repoPath)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to package app: %s", string(output))
	}

	scopedLog.Info("App packaged successfully: %s", output)
	return nil
}

// Create an AWS session with IRSA authentication
func createAWSSession(region, irsaRoleArn string) *session.Session {
	return session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region),
	}))
}

// Upload the packaged app to S3
func uploadToS3(sess *session.Session, bucket, appName, version string) error {
	svc := s3.New(sess)
	filePath := fmt.Sprintf("%s.tar.gz", appName)
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = svc.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fmt.Sprintf("splunk-apps/%s-%s.tar.gz", appName, version)),
		Body:   file,
	})
	return err
}
