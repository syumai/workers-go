// Command grpc-go is a gRPC Worker built with grpc-go, served over the
// inbound TCP socket delivered to the Worker's connect() handler.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	greetv1 "github.com/syumai/workers-go/_examples/grpc-go/gen/greet/v1"
	"github.com/syumai/workers-go/cloudflare/sockets"
)

type greeter struct {
	greetv1.UnimplementedGreeterServiceServer
}

func (g *greeter) SayHello(ctx context.Context, req *greetv1.SayHelloRequest) (*greetv1.SayHelloResponse, error) {
	return &greetv1.SayHelloResponse{
		Name: fmt.Sprintf("Hello, %s!", req.GetName()),
	}, nil
}

func (g *greeter) Chat(stream grpc.BidiStreamingServer[greetv1.ChatRequest, greetv1.ChatResponse]) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := stream.Send(&greetv1.ChatResponse{
			Message: "echo: " + req.GetMessage(),
		}); err != nil {
			return err
		}
	}
}

func main() {
	// One inbound TCP connection = one invocation; the connection stays
	// open for as long as the gRPC client keeps it (see the README for
	// caveats). No keepalive/idle tuning is applied here.
	s := grpc.NewServer()
	greetv1.RegisterGreeterServiceServer(s, &greeter{})
	healthpb.RegisterHealthServer(s, health.NewServer())
	// Registered so grpcurl (and other clients) can call methods, e.g.
	// grpc.health.v1.Health/Check, without a local .proto file.
	reflection.Register(s)

	// sockets.Listen().Accept() (called from within s.Serve) signals
	// readiness to the runtime, so no explicit workers.Ready() call is
	// needed.
	if err := s.Serve(sockets.Listen()); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Fatal(err)
	}
}
