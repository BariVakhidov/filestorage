package p2p

const (
	IncomingMessage = 0x1
	IncomingStream  = 0x2
	ClosePeer       = 0x3
	FileNotFound    = 0x4
)

const (
	STREAM_READY   = "STREAM_READY"
	FILE_NOT_FOUND = "FILE_NOT_FOUND"
	CLOSE_PEER     = "CLOSE_PEER"
)

// RPC holds any arbitrary data that is being sent over
// the each transport between two nodes in the network
type RPC struct {
	Payload []byte
	From    string
	Action  string
}
