package k8s

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListPods returns all pods in a namespace.
func (c *Client) ListPods(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := c.Client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// ListEvents returns all events in a namespace.
func (c *Client) ListEvents(ctx context.Context, namespace string) ([]corev1.Event, error) {
	list := &corev1.EventList{}
	if err := c.Client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// GetPodLogs fetches logs for a specific container in a pod.
func (c *Client) GetPodLogs(ctx context.Context, namespace, podName, container string, previous bool) (string, error) {
	clientset, err := kubernetes.NewForConfig(c.RestConfig)
	if err != nil {
		return "", err
	}
	options := &corev1.PodLogOptions{
		Container:  container,
		Previous:   previous,
		Timestamps: true,
	}
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, options)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	payload, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
