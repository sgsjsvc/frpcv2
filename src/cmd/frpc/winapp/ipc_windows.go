//go:build windows

package winapp

import (
	"errors"
	"io"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"time"

	"github.com/Microsoft/go-winio"
)

type EmptyRequest struct{}

type ConfigPayload struct {
	Content string
}

type StatusPayload struct {
	ConfigPath    string
	ProxyRunning  bool
	LastError     string
	LastOperation string
}

type pipeClient struct{}

func newPipeListener() (net.Listener, error) {
	return winio.ListenPipe(pipeName, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;AU)",
		InputBufferSize:    4096,
		OutputBufferSize:   4096,
	})
}

func (pipeClient) call(method string, req any, resp any) error {
	timeout := time.Second
	conn, err := winio.DialPipe(pipeName, &timeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := rpc.NewClientWithCodec(jsonrpc.NewClientCodec(conn))
	defer client.Close()
	return client.Call("FrpcService."+method, req, resp)
}

func servePipeConnection(server *rpc.Server, conn io.ReadWriteCloser) {
	server.ServeCodec(jsonrpc.NewServerCodec(conn))
}

func isPipeUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) || errors.Is(err, winio.ErrTimeout)
}
