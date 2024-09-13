package p2p

import (
	"encoding/gob"
	"io"
)

type Decoder interface {
	Decode(io.Reader, *RPC) error
}

type GOBDecoder struct{}

func (dec GOBDecoder) Decode(r io.Reader, msg *RPC) error {
	return gob.NewDecoder(r).Decode(msg)
}

type DefaultDecoder struct{}

func (dec DefaultDecoder) Decode(r io.Reader, rpc *RPC) error {
	peakBuf := make([]byte, 1)

	if _, err := r.Read(peakBuf); err != nil {
		return err
	}

	switch peakBuf[0] {
	//In case of a stream we are not decoding what is being sent over the network
	case IncomingStream:
		rpc.Action = STREAM_READY
		return nil
	case ClosePeer:
		rpc.Action = CLOSE_PEER
		return nil
	case FileNotFound:
		rpc.Action = FILE_NOT_FOUND
		return nil
	}

	buf := make([]byte, 1028)
	n, err := r.Read(buf)

	if err != nil {
		return err
	}

	rpc.Payload = buf[:n]

	return nil
}
