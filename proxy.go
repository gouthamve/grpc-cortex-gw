package main

import (
	"net/http"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const grpcServiceConfig = `{"loadBalancingPolicy":"round_robin"}`

// NewProxy initializes the cortex reverse proxies.
func NewProxy(endpoint string) (http.Handler, error) {
	return newGRPCWriteProxy(endpoint)
}

type grpcProxy struct {
	client httpgrpc.HTTPClient
	conn   *grpc.ClientConn
}

func newGRPCWriteProxy(endpoint string) (*grpcProxy, error) {
	dialOptions := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(grpcServiceConfig),
		grpc.WithInsecure(),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                time.Second * 10,
			Timeout:             time.Second * 5,
			PermitWithoutStream: true,
		}),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			grpc_prometheus.UnaryClientInterceptor,
			middleware.ClientUserHeaderInterceptor,
		)),
	}

	conn, err := grpc.Dial(endpoint, dialOptions...)
	if err != nil {
		return nil, err
	}

	return &grpcProxy{
		client: httpgrpc.NewHTTPClient(conn),
		conn:   conn,
	}, nil
}

// ServeHTTP implements http.Handler
func (g *grpcProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := server.HTTPRequest(r)
	if err != nil {
		errorHandler(w, r, err)
		return
	}

	resp, err := g.client.Handle(r.Context(), req)
	if err != nil {
		// Some errors will actually contain a valid resp, just need to unpack it
		var ok bool
		resp, ok = httpgrpc.HTTPResponseFromError(err)

		if !ok {
			errorHandler(w, r, err)
			return
		}
	}

	if err := server.WriteResponse(w, resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
