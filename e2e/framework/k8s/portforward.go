package k8s

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForward represents an active port-forward session.
type PortForward struct {
	LocalPort int
	stopCh    chan struct{}
	errCh     chan error
}

// Close stops the port-forward session.
func (p *PortForward) Close() error {
	if p == nil {
		return nil
	}
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
	select {
	case err := <-p.errCh:
		if err == nil || err == io.EOF {
			return nil
		}
		return err
	case <-time.After(2 * time.Second):
		return nil
	}
}

// StartPortForward opens a local port that forwards to a pod port.
func (c *Client) StartPortForward(ctx context.Context, namespace, podName string, podPort int) (*PortForward, error) {
	if podPort <= 0 {
		return nil, fmt.Errorf("pod port must be > 0")
	}
	localPort, err := freeLocalPort()
	if err != nil {
		return nil, err
	}

	hostURL, err := url.Parse(c.RestConfig.Host)
	if err != nil {
		return nil, err
	}
	if hostURL.Scheme == "" {
		hostURL.Scheme = "https"
	}
	host := hostURL.Host
	if host == "" {
		host = strings.TrimPrefix(c.RestConfig.Host, "https://")
		host = strings.TrimPrefix(host, "http://")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	serverURL := url.URL{Scheme: hostURL.Scheme, Host: host, Path: path}
	transport, upgrader, err := spdy.RoundTripperFor(c.RestConfig)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", &serverURL)

	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)
	ports := []string{fmt.Sprintf("%d:%d", localPort, podPort)}
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, ports, stopCh, readyCh, io.Discard, io.Discard)
	if err != nil {
		return nil, err
	}

	go func() {
		errCh <- forwarder.ForwardPorts()
	}()

	select {
	case <-readyCh:
		return &PortForward{LocalPort: localPort, stopCh: stopCh, errCh: errCh}, nil
	case err := <-errCh:
		if err == nil {
			err = fmt.Errorf("port-forward failed")
		}
		return nil, err
	case <-ctx.Done():
		close(stopCh)
		return nil, ctx.Err()
	}
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected address type: %T", listener.Addr())
	}
	return addr.Port, nil
}
