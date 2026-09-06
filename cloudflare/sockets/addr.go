package sockets

// Addr is the net.Addr reported for a Workers socket. Network() is always
// "tcp". String() is "host:port" when the runtime reports the address and ""
// when it is unknown (same convention as grpc-go's strAddr and net/http's
// empty Request.RemoteAddr).
type Addr struct {
	addr string
}

func (a Addr) Network() string { return "tcp" }
func (a Addr) String() string  { return a.addr }
