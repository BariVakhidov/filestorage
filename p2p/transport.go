package p2p

import "net"

// Peer is an interface that represent the remote node
type Peer interface {
	net.Conn
	Send([]byte) error
	CloseStream()
}

// Transport is an interface that represent anything
// that handles the communication between nodes in the network.
// This can be of the form (TCP, UDP, websockets ...)
type Transport interface {
	ListenAndAccept() error
	Consume() <-chan RPC
	Close() error
	Dial(string) error
	Addr() string
}
