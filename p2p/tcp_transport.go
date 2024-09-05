package p2p

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

// TCPPeer represents the remote node over a TCP established connection
type TCPPeer struct {
	//conn is the underlying connection of the peer
	net.Conn
	//if we dial and retrieve a connection => outbound == true
	//if we accept and retrieve a connection => outbound == false
	outbound bool

	wg *sync.WaitGroup
}

func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		outbound: outbound,
		Conn:     conn,
		wg:       &sync.WaitGroup{},
	}
}

// Send implements the Peer interface
func (t *TCPPeer) Send(b []byte) error {
	_, err := t.Write(b)
	return err
}

// CloseStream implements the Peer interface
func (t *TCPPeer) CloseStream() {
	t.wg.Done()
}

type TCPTransportOptions struct {
	ListenAddr    string
	HandshakeFunc HandshakeFunc
	Decoder       Decoder
	OnPeer        func(Peer) error
}

type TCPTransport struct {
	TCPTransportOptions
	listener net.Listener
	rpcch    chan RPC
}

func NewTCPTransport(options TCPTransportOptions) *TCPTransport {
	return &TCPTransport{
		TCPTransportOptions: options,
		rpcch:               make(chan RPC, 1024),
	}
}

func (t *TCPTransport) Addr() string {
	return t.ListenAddr
}

// Consume implements the Transport interface, which return read-only channel
// for reading incoming messages received from another peer in the network
func (t *TCPTransport) Consume() <-chan RPC {
	return t.rpcch
}

// Close implements the Transport interface
func (t *TCPTransport) Close() error {
	return t.listener.Close()
}

// Dial implements the Transport interface
func (t *TCPTransport) Dial(addr string) error {
	conn, err := net.Dial("tcp", addr)

	if err != nil {
		return err
	}

	go t.handleConnection(conn, true)

	return nil
}

func (t *TCPTransport) ListenAndAccept() error {
	var err error

	t.listener, err = net.Listen("tcp", t.ListenAddr)
	if err != nil {
		return err
	}

	go t.acceptLoop()

	log.Printf("TCP transport is listening port: %s\n", t.ListenAddr)

	return nil
}

func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()

		if errors.Is(err, net.ErrClosed) {
			return
		}

		if err != nil {
			log.Printf("TCP accept err %s\n", err)
		}

		go t.handleConnection(conn, false)
	}
}

func (t *TCPTransport) handleConnection(conn net.Conn, outbound bool) {
	var err error
	defer func() {
		log.Printf("dropping peer connection: %s\n", err)
		err = conn.Close()
		if err != nil {
			log.Printf("error while closing peer connection %+v\n", err)
		}
	}()

	peer := NewTCPPeer(conn, outbound)
	if err = t.HandshakeFunc(peer); err != nil {
		return
	}

	if t.OnPeer != nil {
		if err = t.OnPeer(peer); err != nil {
			return
		}
	}

	for {
		rpc := RPC{From: conn.RemoteAddr().String()}

		if err = t.Decoder.Decode(conn, &rpc); err != nil {
			log.Printf("[%s] TCP error: %s\n", conn.LocalAddr(), err)
			return
		}

		if rpc.Stream {
			peer.wg.Add(1)
			fmt.Printf("[%s] incoming stream [%s], waiting...\n", conn.LocalAddr(), conn.RemoteAddr())
			peer.wg.Wait()
			fmt.Printf("[%s] incoming stream [%s]closed\n", conn.LocalAddr(), conn.RemoteAddr())
			continue
		}

		t.rpcch <- rpc
	}
}
