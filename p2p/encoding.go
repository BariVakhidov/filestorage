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

	//In case of a stream we are not decoding what is being sent over the network
	stream := peakBuf[0] == IncomingStream
	if stream {
		rpc.Stream = true
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
