//go:build js && wasm

// Command websocket is an e2e fixture Worker that echoes WebSocket messages
// the same way _examples/websocket-echo does: Upgrade, then a goroutine
// whose defer calls Conn.Close. Handlers do not assert; the e2e test checks
// that a client Close frame is reciprocated.
package main

import (
	"context"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/websocket"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/ws", handleWebSocket)

	workers.ServeNonBlock(mux)
	workers.Ready()
	<-workers.Done()
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Upgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Read the query before returning. The 101 is sent only after this
	// handler returns, and the request must not be touched from the
	// goroutine that outlives it.
	closeAfterEcho := r.URL.Query().Get("mode") == "close-after-echo"
	go echoLoop(conn, closeAfterEcho)
}

func echoLoop(conn *websocket.Conn, closeAfterEcho bool) {
	defer conn.Close(0, "")
	ctx := context.Background()
	for {
		mt, data, err := conn.ReadMessage(ctx)
		if err != nil {
			return
		}
		if err := conn.WriteMessage(mt, data); err != nil {
			return
		}
		if closeAfterEcho {
			// Explicit Close, then the deferred Close(0, ""): the second
			// call must be a no-op and must not disturb this frame.
			_ = conn.Close(1000, "server")
			return
		}
	}
}
