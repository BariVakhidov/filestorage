package p2p

import "net"

// RPC holds any arbitrary data that is being sent over
// the each transport between two nodes in the network
type RPC struct {
	Payload []byte
	From    net.Addr
}
