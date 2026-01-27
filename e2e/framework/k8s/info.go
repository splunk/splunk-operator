package k8s

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterInfo captures high-level cluster metadata.
type ClusterInfo struct {
	KubernetesVersion      string
	NodeOSImage            string
	ContainerRuntime       string
	KubeletVersion         string
}

// GetClusterInfo returns cluster metadata.
func (c *Client) GetClusterInfo(ctx context.Context) (ClusterInfo, error) {
	clientset, err := kubernetes.NewForConfig(c.RestConfig)
	if err != nil {
		return ClusterInfo{}, err
	}

	info := ClusterInfo{}
	version, err := clientset.Discovery().ServerVersion()
	if err == nil {
		info.KubernetesVersion = version.GitVersion
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil && len(nodes.Items) > 0 {
		node := nodes.Items[0]
		info.NodeOSImage = node.Status.NodeInfo.OSImage
		info.ContainerRuntime = node.Status.NodeInfo.ContainerRuntimeVersion
		info.KubeletVersion = node.Status.NodeInfo.KubeletVersion
	}

	return info, nil
}
