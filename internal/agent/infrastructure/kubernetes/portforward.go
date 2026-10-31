package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	k8sportforward "k8s.io/client-go/tools/portforward"
	k8sspdy "k8s.io/client-go/transport/spdy"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// portForward opens a tunnel from a free loopback port to the pod's binding
// port and returns the local address with the function that closes it.
func (e *Workspace) portForward(ctx context.Context, pod string) (string, func(), error) {
	transport, upgrader, err := k8sspdy.RoundTripperFor(e.rest)
	if err != nil {
		return "", nil, fmt.Errorf("preparing the port-forward: %w", err)
	}
	url := e.typed.CoreV1().RESTClient().Post().Resource("pods").Namespace(e.entry.Namespace).Name(pod).SubResource("portforward").URL()
	dialer := k8sspdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)
	stop := make(chan struct{})
	ready := make(chan struct{})
	forwarder, err := k8sportforward.New(dialer, []string{"0:" + strconv.Itoa(bindingPort)}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		return "", nil, fmt.Errorf("creating the port-forward: %w", err)
	}
	failed := make(chan error, 1)
	go func() { failed <- forwarder.ForwardPorts() }()
	select {
	case <-ready:
	case err := <-failed:
		return "", nil, fmt.Errorf("port-forwarding to pod %s: %w", pod, err)
	case <-ctx.Done():
		close(stop)
		return "", nil, ctx.Err()
	}
	ports, err := forwarder.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stop)
		return "", nil, fmt.Errorf("reading the port-forward's local port: %w", err)
	}
	once := sync.OnceFunc(func() { close(stop) })
	logger.Debug("port-forward opened to the orchestrator pod", "pod", pod, "local_port", ports[0].Local)
	return portAddr(int(ports[0].Local)), once, nil
}
