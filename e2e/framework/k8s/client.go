package k8s

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// Client wraps a controller-runtime client and REST config.
type Client struct {
	Client     client.Client
	RestConfig *rest.Config
	Scheme     *runtime.Scheme
}

// NewClient builds a Kubernetes client for the given kubeconfig.
func NewClient(kubeconfig string) (*Client, error) {
	var cfg *rest.Config
	var err error
	if strings.TrimSpace(kubeconfig) != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = config.GetConfig()
	}
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = enterpriseApi.AddToScheme(scheme)
	_ = enterpriseApiV3.AddToScheme(scheme)

	kubeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return &Client{Client: kubeClient, RestConfig: cfg, Scheme: scheme}, nil
}

// EnsureNamespace creates a namespace if it does not exist.
func (c *Client) EnsureNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{}
	key := client.ObjectKey{Name: name}
	if err := c.Client.Get(ctx, key, ns); err == nil {
		return nil
	}

	obj := &corev1.Namespace{}
	obj.Name = name
	return c.Client.Create(ctx, obj)
}

// DeleteNamespace deletes a namespace.
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{}
	ns.Name = name
	return c.Client.Delete(ctx, ns)
}

// Exec executes a command inside a pod.
func (c *Client) Exec(ctx context.Context, namespace, podName, container string, cmd []string, stdin string, tty bool) (string, string, error) {
	pod := &corev1.Pod{}
	if err := c.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return "", "", err
	}
	gvk, _ := apiutil.GVKForObject(pod, c.Scheme)
	restClient, err := apiutil.RESTClientForGVK(gvk, false, c.RestConfig, serializer.NewCodecFactory(c.Scheme), http.DefaultClient)
	if err != nil {
		return "", "", err
	}

	execReq := restClient.Post().Resource("pods").Name(podName).Namespace(namespace).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command: cmd,
		Stdin:   stdin != "",
		Stdout:  true,
		Stderr:  true,
		TTY:     tty,
	}
	if container != "" {
		option.Container = container
	}

	execReq.VersionedParams(option, runtime.NewParameterCodec(c.Scheme))
	exec, err := remotecommand.NewSPDYExecutor(c.RestConfig, "POST", execReq.URL())
	if err != nil {
		return "", "", err
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	var stdinReader *strings.Reader
	if stdin != "" {
		stdinReader = strings.NewReader(stdin)
	}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdinReader,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
	if err != nil {
		return "", "", fmt.Errorf("exec failed: %w", err)
	}

	return stdout.String(), stderr.String(), nil
}
