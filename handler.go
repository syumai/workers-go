//go:build !js

package workers

import (
	"fmt"
	"net/http"
	"os"
)

// Server serves http.Handler as a normal HTTP server.
// if the given handler is nil, http.DefaultServeMux will be used.
// As a port number, PORT environment variable or default value (9900) is used.
// This function is implemented for non-JS environments for debugging purposes.
func Serve(handler http.Handler) {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "9900"
	}
	addr := fmt.Sprintf(":%s", port)
	fmt.Printf("listening on: http://localhost%s\n", addr)
	fmt.Fprintln(os.Stderr, "warn: this server is currently running in non-JS mode. to enable JS-related features, please use the make command in the syumai/workers-go template.")
	http.ListenAndServe(addr, handler)
}

func ServeNonBlock(http.Handler) {
	panic("ServeNonBlock is not supported in non-JS environments")
}

func Ready() {
	panic("Ready is not supported in non-JS environments")
}

func Done() <-chan struct{} {
	panic("Done is not supported in non-JS environments")
}
