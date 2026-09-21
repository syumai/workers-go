package main

import (
	"context"
	"io"
	"net"

	"github.com/syumai/workers-go/cloudflare/sockets"
)

func main() {
	sockets.Serve(sockets.HandlerFunc(func(ctx context.Context, conn net.Conn) {
		io.Copy(conn, conn) // echo until the client closes
	}))
}
