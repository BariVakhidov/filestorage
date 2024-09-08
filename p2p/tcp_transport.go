package p2p

import (
	"crypto/tls"
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

	wg        *sync.WaitGroup
	rpcChan   chan RPC
	closeChan chan struct{}
}

func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		outbound:  outbound,
		Conn:      conn,
		wg:        &sync.WaitGroup{},
		rpcChan:   make(chan RPC),
		closeChan: make(chan struct{}),
	}
}

// Send implements the Peer interface
func (t *TCPPeer) Send(b []byte) error {
	_, err := t.Write(b)
	return err
}

// Consume implements the Peer interface
func (t *TCPPeer) Consume() <-chan RPC {
	return t.rpcChan
}

// ClosePeer implements the Peer interface
func (t *TCPPeer) ClosePeer() <-chan struct{} {
	return t.closeChan
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
func (t *TCPTransport) Dial(addr string) (Peer, error) {
	// Load client's certificate and private key
	cert, err := tls.LoadX509KeyPair("certs/client.pem", "certs/client.key")
	if err != nil {
		return nil, err
	}

	config := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // Skip verification for self-signed certificates
	}

	// Establish a TLS connection to the server
	conn, err := tls.Dial("tcp", addr, config)

	if err != nil {
		return nil, err
	}

	return t.handleConnection(conn, true)
}

func (t *TCPTransport) ListenAndAccept() error {
	var err error
	// Load server's certificate and private key
	cert, err := tls.LoadX509KeyPair("certs/server.pem", "certs/server.key")
	if err != nil {
		return err
	}

	// Create a TLS config using the certificate
	config := &tls.Config{Certificates: []tls.Certificate{cert}}

	// Create a TCP listener
	t.listener, err = tls.Listen("tcp", t.ListenAddr, config)
	if err != nil {
		return err
	}

	log.Printf("TCP transport is listening port: %s\n", t.ListenAddr)

	t.acceptLoop()

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

		go func() {
			if _, err := t.handleConnection(conn, false); err != nil {
				log.Printf("connection err: %s\n", err)
			}
		}()
	}
}

func (t *TCPTransport) handleConnection(conn net.Conn, outbound bool) (Peer, error) {
	var err error

	peer := NewTCPPeer(conn, outbound)
	if err = t.HandshakeFunc(peer); err != nil {
		return nil, err
	}

	if t.OnPeer != nil {
		if err = t.OnPeer(peer); err != nil {
			log.Printf("error OnPeer %+v\n", err)
			return nil, err
		}
	}

	go func() {
		// defer func() {
		// 	log.Printf("dropping peer connection: %s\n", err)
		// 	err = conn.Close()
		// 	if err != nil {
		// 		log.Printf("error while closing peer connection %+v\n", err)
		// 	}
		// }()
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

			if rpc.Closed {
				peer.closeChan <- struct{}{}
				return
			}

			peer.rpcChan <- rpc
		}
	}()

	return peer, nil
}
