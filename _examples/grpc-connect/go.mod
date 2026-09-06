module github.com/syumai/workers-go/_examples/grpc-connect

go 1.24

require (
	connectrpc.com/connect v1.18.1
	github.com/syumai/workers-go v0.0.0
	google.golang.org/protobuf v1.36.10
)

replace github.com/syumai/workers-go => ../../
