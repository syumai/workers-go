// This gRPC Worker is built with the standard library's net/http (h2c) and
// connectrpc.com/connect.
//
// One http.Handler serves two triggers from a single binary:
//   - fetch(): Connect / gRPC-Web / (Cloudflare-translated) gRPC over HTTP,
//     via workers.ServeNonBlock.
//   - connect(): native gRPC over HTTP/2 (h2c, prior knowledge) on the
//     inbound TCP socket, via http.Server.Serve(sockets.Listen()).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"connectrpc.com/connect"

	greetv1 "grpc-go/gen/greet/v1"
	"grpc-go/gen/greet/v1/greetv1connect"

	workers "github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/sockets"
)

type greeter struct {
	greetv1connect.UnimplementedGreeterServiceHandler
}

func (g *greeter) SayHello(ctx context.Context, req *connect.Request[greetv1.SayHelloRequest]) (*connect.Response[greetv1.SayHelloResponse], error) {
	return connect.NewResponse(&greetv1.SayHelloResponse{
		Name: fmt.Sprintf("Hello, %s!", req.Msg.Name),
	}), nil
}

func (g *greeter) Chat(ctx context.Context, stream *connect.BidiStream[greetv1.ChatRequest, greetv1.ChatResponse]) error {
	for {
		req, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := stream.Send(&greetv1.ChatResponse{
			Message: "echo: " + req.Message,
		}); err != nil {
			return err
		}
	}
}

func main() {
	mux := http.NewServeMux()
	mux.Handle(greetv1connect.NewGreeterServiceHandler(&greeter{}))

	// fetch(): Connect / gRPC-Web / (Cloudflare-translated) gRPC over HTTP.
	workers.ServeNonBlock(mux)

	// connect(): native gRPC over HTTP/2 (h2c, prior knowledge) on the
	// inbound TCP socket. UnencryptedHTTP2 lets net/http accept an h2c
	// client connection (grpc-go, connect-go, grpcurl -plaintext all
	// connect this way; there is no inbound TLS termination).
	var protos http.Protocols
	protos.SetHTTP1(true)
	protos.SetUnencryptedHTTP2(true)
	srv := &http.Server{Handler: mux, Protocols: &protos}

	// sockets.Listen().Accept() (called from within srv.Serve) signals
	// readiness to the runtime, so no explicit workers.Ready() call is
	// needed here.
	if err := srv.Serve(sockets.Listen()); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Fatal(err)
	}
}
