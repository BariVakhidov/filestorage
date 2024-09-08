package p2p

const (
	IncomingMessage = 0x1
	IncomingStream  = 0x2
	ClosePeer       = 0x3
)

// RPC holds any arbitrary data that is being sent over
// the each transport between two nodes in the network
type RPC struct {
	Payload []byte
	From    string
	Stream  bool
	Closed  bool
}
