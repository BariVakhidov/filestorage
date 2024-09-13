package p2p

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTCPTransport(t *testing.T) {
	listenAddr := ":3000"
	tcpOpts := TCPTransportOptions{
		ListenAddr:    ":3000",
		Decoder:       DefaultDecoder{},
		HandshakeFunc: NOPHandshakeFunc,
	}
	tr, err := NewTCPTransport(tcpOpts)
	//TODO: certs for testing
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, listenAddr, tr.ListenAddr)

	//Server
	assert.Nil(t, tr.ListenAndAccept())
}
