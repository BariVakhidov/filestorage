package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/BariVakhidov/filestorage/p2p"
)

const (
	ACTION_DELETED            = "ACTION_DELETED"
	ACTION_STORE_STREAM_READY = "ACTION_STORE_STREAM_READY"
)

var infoActions = [3]string{ACTION_DELETED, ACTION_STORE_STREAM_READY}

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

type MessageInfo struct {
	Key    string
	Action string
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

	infoWGroups map[string]map[string]*sync.WaitGroup
	infoLock    sync.RWMutex
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

	infoWGroups := make(map[string]map[string]*sync.WaitGroup)
	for _, action := range infoActions {
		infoWGroups[action] = make(map[string]*sync.WaitGroup)
	}

	return &FileServer{
		FileServerOptions: opts,
		store:             store,
		quitch:            make(chan struct{}),
		peers:             make(map[string]p2p.Peer),
		peersLock:         sync.RWMutex{},
		infoWGroups:       infoWGroups,
		infoLock:          sync.RWMutex{},
	}, nil
}

func (s *FileServer) Stop() {
	close(s.quitch)
}

func (s *FileServer) OnPeer(p p2p.Peer) error {
	s.peersLock.Lock()
	defer s.peersLock.Unlock()

	s.peers[p.RemoteAddr().String()] = p

	s.Logger.Info("connected peer", "RemoteAddr", p.RemoteAddr(), "Transport.Addr", s.Transport.Addr())

	return nil
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

func (s *FileServer) loop() {
	defer func() {
		s.Logger.Info("file server stopped by due to error or user action", "Transport.Addr", s.Transport.Addr())
		s.Transport.Close()
	}()

	for {
		select {
		case rpc := <-s.Transport.Consume():
			go func() {
				var msg Message
				if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
					s.Logger.Error("decoding fail", "err", err, "Transport.Addr", s.Transport.Addr())
					return
				}

				if err := s.handleMessage(&msg, rpc.From); err != nil {
					s.Logger.Error("handleMessage fail", "err", err, "Transport.Addr", s.Transport.Addr())
					return
				}
			}()
		case <-s.quitch:
			return
		}
	}
}

func (s *FileServer) handleMessage(m *Message, from string) error {
	switch v := m.Payload.(type) {
	case MessageStoreFile:
		return s.handleMessageStoreFile(v, from)
	case MessageGetFile:
		return s.handleMessageGetFile(v, from)
	case MessageDeleteFile:
		return s.handleMessageDeleteFile(v, from)
	case MessageInfo:
		return s.handleMessageInfo(v)
	}

	return nil
}

func (s *FileServer) handleMessageGetFile(msg MessageGetFile, from string) error {
	if !s.store.Has(msg.ID, msg.Key) {
		return fmt.Errorf("[%s] need to serve file (%s), but does not exists on the disk", s.Transport.Addr(), msg.Key)
	}

	fmt.Printf("[%s] serving file (%s) over the network\n", s.Transport.Addr(), msg.Key)
	fileSize, r, err := s.store.Read(msg.ID, msg.Key)

	if err != nil {
		return err
	}

	peer, ok := s.peers[from]
	if !ok {
		return NotFoundPeerError{From: from}
	}

	if err := peer.Send([]byte{p2p.IncomingStream}); err != nil {
		return err
	}
	if err := binary.Write(peer, binary.LittleEndian, fileSize+16); err != nil {
		return err
	}

	n, err := copyEncrypt(s.EncKey, r, peer)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes over the network %s\n", s.Transport.Addr(), n, from)

	return nil
}

func (s *FileServer) handleMessageInfo(msg MessageInfo) error {
	s.infoLock.RLock()
	defer s.infoLock.RUnlock()
	wg, ok := s.infoWGroups[msg.Action][msg.Key]
	if !ok {
		s.Logger.Error("stream not found", "id", msg.Key, "Transport.Addr", s.Transport.Addr())
		return nil
	}
	wg.Done()
	return nil
}

func (s *FileServer) handleMessageDeleteFile(msg MessageDeleteFile, from string) error {
	peer, ok := s.peers[from]
	if !ok {
		return NotFoundPeerError{From: from}
	}

	if err := s.store.Delete(msg.ID, msg.KeyToDelete); err != nil {
		return err
	}

	messageDeleted := &Message{
		Payload: MessageInfo{
			Key:    msg.KeyToDelete,
			Action: ACTION_DELETED,
		},
	}
	return s.sendMessage(messageDeleted, peer)
}

func (s *FileServer) handleMessageStoreFile(msg MessageStoreFile, from string) error {
	peer, ok := s.peers[from]
	if !ok {
		return NotFoundPeerError{From: from}
	}

	defer peer.CloseStream()

	messageReady := &Message{
		Payload: MessageInfo{
			Key:    msg.Key,
			Action: ACTION_STORE_STREAM_READY,
		},
	}
	if err := s.sendMessage(messageReady, peer); err != nil {
		return err
	}

	n, err := s.store.WriteDecrypt(s.EncKey, msg.ID, msg.Key, io.LimitReader(peer, msg.Size))
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes to the disk with id %s\n", s.Transport.Addr(), n, msg.ID)

	return nil
}

func (s *FileServer) bootstrapNetwork() error {
	if len(s.BootstrapNodes) == 0 {
		return nil
	}

	for _, addr := range s.BootstrapNodes {
		if len(addr) == 0 {
			continue
		}

		go func(addr string) {
			if err := s.Transport.Dial(addr); err != nil {
				s.Logger.Error("dial", "err", err, "Transport.Addr", s.Transport.Addr())
			}
		}(addr)
	}

	return nil
}

func (s *FileServer) Start() error {
	if err := s.Transport.ListenAndAccept(); err != nil {
		return err
	}

	if err := s.bootstrapNetwork(); err != nil {
		return err
	}

	s.loop()

	return nil
}

func (s *FileServer) broadcast(msg *Message, key string, action string) error {
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	s.infoWGroups[action][key] = &sync.WaitGroup{}

	for _, peer := range s.peers {
		s.infoWGroups[action][key].Add(1)
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
		}()
	}

	s.infoWGroups[action][key].Wait()
	delete(s.infoWGroups[action], key)

	return nil
}

// TODO: open new connection
func (s *FileServer) Store(key string, r io.Reader) error {
	var (
		fileBuffer = new(bytes.Buffer)
		tee        = io.TeeReader(r, fileBuffer)
	)

	hashedKey := hashKey(key)

	size, err := s.store.Write(s.ID, hashedKey, tee)
	if err != nil {
		return err
	}

	msg := &Message{
		Payload: MessageStoreFile{
			Key:  hashedKey,
			Size: size + 16,
			ID:   s.ID,
		},
	}

	if err := s.broadcast(msg, hashedKey, ACTION_STORE_STREAM_READY); err != nil {
		return err
	}

	//p2p.Peer -> io.Writer
	peers := make([]io.Writer, 0, len(s.peers))
	for _, peer := range s.peers {
		peers = append(peers, peer)
	}

	mw := io.MultiWriter(peers...)
	if _, err := mw.Write([]byte{p2p.IncomingStream}); err != nil {
		return err
	}
	//write file to all peers with io.MultiWriter
	n, err := copyEncrypt(s.EncKey, fileBuffer, mw)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] received and written (%d) bytes to the disk\n", s.Transport.Addr(), n)

	return nil
}

func (s *FileServer) Get(key string) (int64, io.Reader, error) {
	hashedKey := hashKey(key)

	if s.store.Has(s.ID, hashedKey) {
		fmt.Printf("[%s] serving file (%s) from the local disk\n", s.Transport.Addr(), hashedKey)
		return s.store.Read(s.ID, hashedKey)
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

	for _, peer := range s.peers {
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
		}()
	}

	wg := &sync.WaitGroup{}
	for _, peer := range s.peers {
		wg.Add(1)
		go func() {
			defer func() {
				peer.CloseStream()
				wg.Done()
			}()

			var fileSize int64
			if err := binary.Read(peer, binary.LittleEndian, &fileSize); err != nil {
				return
			}

			n, err := s.store.WriteDecrypt(s.EncKey, s.ID, hashedKey, io.LimitReader(peer, fileSize))
			if err != nil {
				return
			}

			fmt.Printf("[%s] received (%d) bytes over the network %s\n", s.Transport.Addr(), n, peer.RemoteAddr())
		}()
	}
	wg.Wait()
	return s.store.Read(s.ID, hashedKey)
}

func (s *FileServer) Delete(key string) error {
	hashedKey := hashKey(key)

	if err := s.store.Delete(s.ID, hashedKey); err != nil {
		return err
	}

	msg := &Message{
		Payload: MessageDeleteFile{
			KeyToDelete: hashedKey,
			ID:          s.ID,
		},
	}

	return s.broadcast(msg, hashedKey, ACTION_DELETED)
}

func init() {
	var once sync.Once
	once.Do(func() {
		gob.Register(MessageStoreFile{})
		gob.Register(MessageGetFile{})
		gob.Register(MessageDeleteFile{})
		gob.Register(MessageInfo{})
	})
}
