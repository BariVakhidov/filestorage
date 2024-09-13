package p2p

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math"
	"net"
	"os"
	"sync"
	"time"
)

const (
	network    = "tcp"
	maxBackoff = time.Second * 10
)

// TCPPeer represents the remote node over a TCP established connection
type TCPPeer struct {
	//conn is the underlying connection of the peer
	net.Conn
	//if we dial and retrieve a connection => outbound == true
	//if we accept and retrieve a connection => outbound == false
	outbound bool

	wg         *sync.WaitGroup
	rpcChan    chan RPC
	streamChan chan bool
	closeChan  chan struct{}
}

func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		outbound:   outbound,
		Conn:       conn,
		wg:         &sync.WaitGroup{},
		rpcChan:    make(chan RPC, 1024),
		streamChan: make(chan bool),
		closeChan:  make(chan struct{}),
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

// CloseStream implements the Peer interface
func (t *TCPPeer) CloseStream() {
	t.wg.Done()
}

func (t *TCPPeer) ConsumeStream() <-chan bool {
	return t.streamChan
}

func (t *TCPPeer) ClosePeer() {
	t.closeChan <- struct{}{}
}

func (t *TCPPeer) ConsumeClosePeer() <-chan struct{} {
	return t.closeChan
}

type TCPTransportOptions struct {
	ListenAddr    string
	HandshakeFunc HandshakeFunc
	Decoder       Decoder
	OnPeer        func(Peer) error
	OnReconnect   func(Peer) error
	Logger        *slog.Logger
}

type TCPTransport struct {
	TCPTransportOptions
	listener     net.Listener
	clientConfig *tls.Config
}

func NewTCPTransport(options TCPTransportOptions) (*TCPTransport, error) {
	// Load client's certificate and private key
	cert, err := tls.LoadX509KeyPair("certs/client.pem", "certs/client.key")
	if err != nil {
		return nil, err
	}

	config := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // Skip verification for self-signed certificates
	}

	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	return &TCPTransport{
		TCPTransportOptions: options,
		clientConfig:        config,
	}, nil
}

func (t *TCPTransport) Addr() string {
	return t.ListenAddr
}

// Close implements the Transport interface
func (t *TCPTransport) Close() error {
	return t.listener.Close()
}

func (t *TCPTransport) tryReconnect(network, addr string) {
	dial := func() error {
		conn, err := tls.Dial(network, addr, t.clientConfig)
		if err != nil {
			return err
		}

		fmt.Printf("[%s] successfully reconnected to [%s]...\n", t.Addr(), addr)

		peer := NewTCPPeer(conn, true)
		if err := t.OnReconnect(peer); err != nil {
			if connErr := peer.Close(); connErr != nil {
				log.Printf("[%s] error while closing peer connection %+v\n", t.Addr(), connErr)
			}
			return err
		}

		go t.handleConnection(peer)

		return nil
	}

	go func() {
		fmt.Printf("[%s] trying to reconnect to [%s]...\n", t.Addr(), addr)

		var (
			count     = 1
			baseDelay = time.Millisecond * 100
			ticker    = time.NewTicker(baseDelay)
			timer     = time.NewTimer(time.Minute * 5)
		)

		defer ticker.Stop()
		defer timer.Stop()

		for {
			select {
			case <-ticker.C:
				if err := dial(); err != nil {
					fmt.Printf("[%s] error [%s] while reconnecting [%d] to [%s]...\n", t.Addr(), err, count, addr)
					//Exponential Backoff
					backoff := time.Duration(math.Pow(2, float64(count))) * baseDelay
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					ticker.Reset(backoff)
					count++
					continue
				}
				return
			case <-timer.C:
				fmt.Printf("[%s] failed to reconnect to [%s]...\n", t.Addr(), addr)
				return
			}
		}
	}()
}

// Dial implements the Transport interface
func (t *TCPTransport) Dial(addr string) (Peer, error) {
	// Establish a TLS connection to the server
	conn, err := tls.Dial(network, addr, t.clientConfig)

	if err != nil {
		t.tryReconnect(network, addr)
		return nil, err
	}

	peer := NewTCPPeer(conn, true)
	go t.handleConnection(peer)

	return peer, nil
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

	t.Logger.Info("TCP transport start listening", "port", t.ListenAddr)

	go t.acceptLoop()

	return nil
}

func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()

		if errors.Is(err, net.ErrClosed) {
			return
		}

		if err != nil {
			t.Logger.Error("TCP accept", "err", err)
			continue
		}

		peer := NewTCPPeer(conn, false)
		go t.handleConnection(peer)
	}
}

func (t *TCPTransport) handleConnection(peer *TCPPeer) {
	var err error
	defer func() {
		if errors.Is(err, net.ErrClosed) {
			peer.closeChan <- struct{}{}
			return
		}
		log.Printf("[%s] dropping peer connection: %s\n", t.Addr(), err)
		if err = peer.Close(); err != nil {
			log.Printf("[%s] error while closing peer connection %+v\n", t.Addr(), err)
		}
	}()

	if err = t.HandshakeFunc(peer); err != nil {
		return
	}

	if t.OnPeer != nil {
		if err = t.OnPeer(peer); err != nil {
			log.Printf("error OnPeer %+v\n", err)
			return
		}
	}

	for {
		rpc := RPC{From: peer.RemoteAddr().String()}
		if err = t.Decoder.Decode(peer, &rpc); err != nil {
			log.Printf("[%s] TCP error: %s\n", peer.LocalAddr(), err)
			if errors.Is(err, io.EOF) {
				//TODO
				t.tryReconnect(network, peer.RemoteAddr().String())
			}
			return
		}

		if rpc.Action == "" {
			peer.rpcChan <- rpc
			continue
		}

		switch rpc.Action {
		case STREAM_READY:
			peer.wg.Add(1)
			peer.streamChan <- true
			peer.wg.Wait()
			continue
		case CLOSE_PEER:
			err = fmt.Errorf("Shutdown message received. Closing connection.")
			peer.closeChan <- struct{}{}
			return
		//TODO
		default:
			peer.streamChan <- false
		}
	}
}
