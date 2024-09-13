package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/BariVakhidov/filestorage/p2p"
)

const (
	ACTION_PING = "ACTION_PING"
	ACTION_PONG = "ACTION_PONG"
)

type NotFoundPeerError struct {
	From string
}

func (n NotFoundPeerError) Error() string {
	return fmt.Sprintf("peer (%s) could not be found in the peer list", n.From)
}

type Message struct {
	Payload any
}

type MessageStoreFile struct {
	Key  string
	Size int64
	ID   string
}

type MessageGetFile struct {
	Key string
	ID  string
}

type MessageDeleteFile struct {
	KeyToDelete string
	ID          string
}

type MessagePing struct {
	PingAction string
}

type FileServerOptions struct {
	StorageRoot       string
	PathTransformFunc PathTransformFunc
	Transport         p2p.Transport
	BootstrapNodes    []string
	EncKey            []byte
	//ID of the owner of the storage
	ID     string
	Logger *slog.Logger
}

type FileServer struct {
	FileServerOptions
	store  *Storage
	quitch chan struct{}

	peers     map[string]p2p.Peer
	peersLock sync.RWMutex

	activeNodes     map[string]struct{}
	activeNodesLock sync.RWMutex
}

func NewFileServer(opts FileServerOptions) (*FileServer, error) {
	storeOpts := StorageOptions{
		Root:              opts.StorageRoot,
		PathTransformFunc: opts.PathTransformFunc,
	}

	store := NewStorage(storeOpts)

	if len(opts.ID) == 0 {
		id, err := generateID()
		if err != nil {
			return nil, err
		}
		opts.ID = id
	}

	return &FileServer{
		FileServerOptions: opts,
		store:             store,
		quitch:            make(chan struct{}),
		peers:             make(map[string]p2p.Peer),
		peersLock:         sync.RWMutex{},
		activeNodesLock:   sync.RWMutex{},
		activeNodes:       make(map[string]struct{}),
	}, nil
}

func (s *FileServer) Stop() {
	//TODO
	s.Transport.Close()
	close(s.quitch)
}

func (s *FileServer) OnPeer(p p2p.Peer) error {
	s.Logger.Info("connected peer", "RemoteAddr", p.RemoteAddr(), "Transport.Addr", s.Transport.Addr())

	s.activeNodesLock.Lock()
	s.activeNodes[p.RemoteAddr().String()] = struct{}{}
	s.activeNodesLock.Unlock()

	go s.peerLoop(p)

	return nil
}

func (s *FileServer) closePeer(peer p2p.Peer) error {
	//Send close message to remote node
	if err := peer.Send([]byte{p2p.ClosePeer}); err != nil {
		s.Logger.Error("peer.Send IncomingMessage", "err", err)
		return err
	}

	return peer.Close()
}

func (s *FileServer) sendMessage(msg *Message, peer p2p.Peer) error {
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	if err := peer.Send([]byte{p2p.IncomingMessage}); err != nil {
		s.Logger.Error("peer.Send IncomingMessage", "err", err)
		return err
	}

	if err := peer.Send(buf.Bytes()); err != nil {
		s.Logger.Error("peer.Send msg", "err", err)
		return err
	}

	return nil
}

func (s *FileServer) peerLoop(peer p2p.Peer) {
	for {
		select {
		case rpc := <-peer.Consume():
			go func() {
				var msg Message
				if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
					s.Logger.Error("decoding fail", "err", err, "Transport.Addr", s.Transport.Addr())
					return
				}

				if err := s.handleMessage(&msg, peer); err != nil {
					s.Logger.Error("handleMessage fail", "err", err, "Transport.Addr", s.Transport.Addr())
				}
			}()
		case <-peer.ConsumeClosePeer():
			return
		case <-s.quitch:
			//In case of stopping server we need to close all peers
			s.Logger.Info("Closing connection", "Transport.Addr", s.Transport.Addr(), "peer", peer.RemoteAddr().String())
			if err := peer.Close(); !errors.Is(err, net.ErrClosed) {
				s.Logger.Error("Closing peer", "Transport.Addr", s.Transport.Addr(), "peer", peer.RemoteAddr().String(), "err", err)
			}
			return
		}
	}
}

func (s *FileServer) handleMessage(m *Message, peer p2p.Peer) error {
	switch v := m.Payload.(type) {
	case MessageStoreFile:
		return s.handleMessageStoreFile(v, peer)
	case MessageGetFile:
		return s.handleMessageGetFile(v, peer)
	case MessageDeleteFile:
		return s.handleMessageDeleteFile(v, peer)
	case MessagePing:
		return s.handleMessagePing(v, peer)
	}

	return nil
}

func (s *FileServer) handleMessageGetFile(msg MessageGetFile, peer p2p.Peer) error {
	if !s.store.Has(msg.ID, msg.Key) {
		if err := peer.Send([]byte{p2p.FileNotFound}); err != nil {
			s.Logger.Error("peer.Send IncomingMessage", "err", err)
			return err
		}
		return fmt.Errorf("[%s] need to serve file (%s), but does not exists on the disk", s.Transport.Addr(), msg.Key)
	}

	fileSize, r, err := s.store.Read(msg.ID, msg.Key)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] serving file (%s) over the network\n", s.Transport.Addr(), msg.Key)

	if err := peer.Send([]byte{p2p.IncomingStream}); err != nil {
		return err
	}

	if err := binary.Write(peer, binary.LittleEndian, fileSize); err != nil {
		return err
	}

	n, err := io.Copy(peer, r)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes over the network %s\n", s.Transport.Addr(), n, peer.RemoteAddr().String())

	return nil
}

func (s *FileServer) handleMessagePing(msg MessagePing, peer p2p.Peer) error {
	switch msg.PingAction {
	case ACTION_PING:
		msg := &Message{
			Payload: MessagePing{
				PingAction: ACTION_PONG,
			},
		}
		fmt.Printf("[%s] PING FROM [%s]\n", s.Transport.Addr(), peer.RemoteAddr().String())

		//Update peers for pinging
		s.peersLock.RLock()
		_, ok := s.peers[peer.RemoteAddr().String()]
		s.peersLock.RUnlock()
		if !ok {
			s.peersLock.Lock()
			s.peers[peer.RemoteAddr().String()] = peer
			s.peersLock.Unlock()
		}

		return s.sendMessage(msg, peer)
	case ACTION_PONG:
		fmt.Printf("[%s] PONG from [%s]\n", s.Transport.Addr(), peer.RemoteAddr().String())
	}
	return nil
}

func (s *FileServer) handleMessageDeleteFile(msg MessageDeleteFile, peer p2p.Peer) error {
	if err := s.store.Delete(msg.ID, msg.KeyToDelete); err != nil {
		return err
	}

	return s.closePeer(peer)
}

func (s *FileServer) handleMessageStoreFile(msg MessageStoreFile, peer p2p.Peer) error {
	<-peer.ConsumeStream()

	defer peer.CloseStream()

	n, err := s.store.WriteEncrypt(s.EncKey, msg.ID, msg.Key, io.LimitReader(peer, msg.Size))
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes to the disk with id %s\n", s.Transport.Addr(), n, msg.ID)

	return s.closePeer(peer)
}

func (s *FileServer) dialNodes() ([]p2p.Peer, error) {
	s.activeNodesLock.RLock()
	nodes := s.activeNodes
	s.activeNodesLock.RUnlock()

	if len(nodes) == 0 {
		return nil, nil
	}

	peers := make([]p2p.Peer, 0, len(nodes))
	mu := &sync.Mutex{}
	wg := &sync.WaitGroup{}

	for addr := range nodes {
		if len(addr) == 0 {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			peer, err := s.Transport.Dial(addr)
			if err != nil {
				s.Logger.Error("dial", "err", err, "Transport.Addr", s.Transport.Addr())
				s.activeNodesLock.Lock()
				delete(s.activeNodes, addr)
				s.activeNodesLock.Unlock()
				return
			}

			mu.Lock()
			peers = append(peers, peer)
			mu.Unlock()
		}()
	}
	wg.Wait()

	return peers, nil
}

func (s *FileServer) bootstrapNetwork() error {
	if len(s.BootstrapNodes) == 0 {
		return nil
	}

	wg := &sync.WaitGroup{}

	for _, addr := range s.BootstrapNodes {
		if len(addr) == 0 {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			peer, err := s.Transport.Dial(addr)
			if err != nil {
				s.Logger.Error("dial", "err", err, "Transport.Addr", s.Transport.Addr())
				return
			}

			s.peersLock.Lock()
			s.peers[peer.RemoteAddr().String()] = peer
			s.peersLock.Unlock()
		}()
	}

	wg.Wait()
	return nil
}

func (s *FileServer) OnReconnect(peer p2p.Peer) error {
	remoteAddr := peer.RemoteAddr().String()

	s.peersLock.Lock()
	s.peers[remoteAddr] = peer
	s.peersLock.Unlock()

	return nil
}

func (s *FileServer) pingPeers(msg *Message) {
	s.peersLock.RLock()
	defer s.peersLock.RUnlock()

	if len(s.peers) == 0 {
		return
	}

	peersAddr := make([]string, 0, len(s.peers))
	for addr := range s.peers {
		peersAddr = append(peersAddr, addr)
	}

	fmt.Printf("[%s] PING %+v nodes\n", s.Transport.Addr(), peersAddr)

	wg := &sync.WaitGroup{}
	wg.Add(len(s.peers))

	for addr, peer := range s.peers {
		go func() {
			defer wg.Done()
			if err := s.sendMessage(msg, peer); err != nil {
				fmt.Printf("[%s] PING [%s] err %s\n", s.Transport.Addr(), addr, err)
				s.peersLock.Lock()
				delete(s.peers, addr)
				s.peersLock.Unlock()
				s.activeNodesLock.Lock()
				delete(s.activeNodes, addr)
				s.activeNodesLock.Unlock()
			}
		}()
	}

	wg.Wait()
}

func (s *FileServer) ping() error {
	msg := &Message{
		Payload: MessagePing{
			PingAction: ACTION_PING,
		},
	}

	s.pingPeers(msg)

	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Printf("[%s]------------------> before ping\n", s.Transport.Addr())
			s.pingPeers(msg)
		case <-s.quitch:
			return nil
		}
	}
}

// TODO
func (s *FileServer) Sync() error {
	return nil
}

func (s *FileServer) Start() error {
	if err := s.Transport.ListenAndAccept(); err != nil {
		return err
	}

	if err := s.bootstrapNetwork(); err != nil {
		return err
	}

	if err := s.ping(); err != nil {
		return err
	}

	return nil
}

func (s *FileServer) broadcast(peers []p2p.Peer, msg *Message) error {
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	wg := &sync.WaitGroup{}
	wg.Add(len(peers))

	for _, peer := range peers {
		go func() {
			//TODO: retry sending
			if err := peer.Send([]byte{p2p.IncomingMessage}); err != nil {
				s.Logger.Error("peer.Send IncomingMessage", "err", err)
				return
			}
			if err := peer.Send(buf.Bytes()); err != nil {
				s.Logger.Error("peer.Send msg", "err", err)
				return
			}
			wg.Done()
		}()
	}

	wg.Wait()
	return nil
}

func (s *FileServer) storeToNetwork(peers []p2p.Peer, hashedKey string, size int64, r io.Reader) error {
	msg := &Message{
		Payload: MessageStoreFile{
			Key:  hashedKey,
			Size: size - 16,
			ID:   s.ID,
		},
	}

	if err := s.broadcast(peers, msg); err != nil {
		return err
	}

	//p2p.Peer -> io.Writer
	writers := make([]io.Writer, 0, len(s.BootstrapNodes))
	for _, peer := range peers {
		writers = append(writers, peer)
	}

	mw := io.MultiWriter(writers...)
	if _, err := mw.Write([]byte{p2p.IncomingStream}); err != nil {
		return err
	}
	//write file to all peers with io.MultiWriter
	n, err := io.Copy(mw, r)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes to peers\n", s.Transport.Addr(), n)

	return nil
}

func (s *FileServer) getActiveNodes() []string {
	s.activeNodesLock.RLock()
	defer s.activeNodesLock.RUnlock()

	activeNodes := make([]string, 0, len(s.activeNodes))
	for node := range s.activeNodes {
		activeNodes = append(activeNodes, node)
	}

	return activeNodes
}

func (s *FileServer) Store(key string, r io.Reader) error {
	var (
		fileBuffer = new(bytes.Buffer)
		tee        = io.TeeReader(r, fileBuffer)
	)

	hashedKey := hashKey(key)

	size, err := s.store.WriteEncrypt(s.EncKey, s.ID, hashedKey, tee)
	if err != nil {
		return err
	}
	fmt.Printf("[%s] written (%d) bytes to the disk\n", s.Transport.Addr(), size)

	peers, err := s.dialNodes()
	if err != nil {
		return err
	}

	return s.storeToNetwork(peers, hashedKey, size, fileBuffer)
}

func (s *FileServer) Get(key string) (int64, io.Reader, error) {
	hashedKey := hashKey(key)

	if s.store.Has(s.ID, hashedKey) {
		fmt.Printf("[%s] serving file (%s) from the local disk\n", s.Transport.Addr(), hashedKey)
		return s.store.ReadDecrypt(s.EncKey, s.ID, hashedKey)
	}

	fmt.Printf("[%s] don't have file locally (%s), fetching from the network...\n", s.Transport.Addr(), hashedKey)

	msg := &Message{
		Payload: MessageGetFile{
			Key: hashedKey,
			ID:  s.ID,
		},
	}

	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return 0, nil, err
	}

	var (
		activeNodes = s.getActiveNodes()
		peersToSync = make([]p2p.Peer, 0)
	)

	//Trying to find first peer with requested file
	for _, node := range activeNodes {
		peer, err := s.Transport.Dial(node)
		if err != nil {
			return 0, nil, err
		}

		if err := peer.Send([]byte{p2p.IncomingMessage}); err != nil {
			s.Logger.Error("peer.Send IncomingMessage", "err", err)
			return 0, nil, err
		}
		if err := peer.Send(buf.Bytes()); err != nil {
			s.Logger.Error("peer.Send msg", "err", err)
			return 0, nil, err
		}

		if streamReady := <-peer.ConsumeStream(); !streamReady {
			peersToSync = append(peersToSync, peer)
			continue
		}

		defer func() {
			peer.CloseStream()
		}()

		var fileSize int64
		if err := binary.Read(peer, binary.LittleEndian, &fileSize); err != nil {
			return 0, nil, err
		}

		n, err := s.store.Write(s.ID, hashedKey, io.LimitReader(peer, fileSize))
		if err != nil {
			return 0, nil, err
		}

		fmt.Printf("[%s] received (%d) bytes over the network %s\n", s.Transport.Addr(), n, peer.RemoteAddr())
		if err := s.closePeer(peer); err != nil {
			return 0, nil, err
		}
	}

	n, r, err := s.store.ReadDecrypt(s.EncKey, s.ID, hashedKey)
	if err != nil {
		return 0, nil, err
	}

	if len(peersToSync) == 0 {
		return n, r, nil
	}

	var (
		fileBuffer = new(bytes.Buffer)
		tee        = io.TeeReader(r, fileBuffer)
	)

	if err := s.storeToNetwork(peersToSync, hashedKey, n, tee); err != nil {
		return 0, nil, err
	}

	return n, fileBuffer, nil
}

func (s *FileServer) Delete(key string) error {
	hashedKey := hashKey(key)

	if err := s.store.Delete(s.ID, hashedKey); err != nil {
		return err
	}

	peers, err := s.dialNodes()
	if err != nil {
		return err
	}

	msg := &Message{
		Payload: MessageDeleteFile{
			KeyToDelete: hashedKey,
			ID:          s.ID,
		},
	}

	return s.broadcast(peers, msg)
}

func init() {
	var once sync.Once
	once.Do(func() {
		gob.Register(MessageStoreFile{})
		gob.Register(MessageGetFile{})
		gob.Register(MessageDeleteFile{})
		gob.Register(MessagePing{})
	})
}
