package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/BariVakhidov/filestorage/p2p"
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
	Key string
	ID  string
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

func (s *FileServer) loop() {
	defer func() {
		s.Logger.Info("file server stopped by due to error or user action", "Transport.Addr", s.Transport.Addr())
		s.Transport.Close()
	}()

	for {
		select {
		case rpc := <-s.Transport.Consume():
			go func(rpc p2p.RPC) {
				var msg Message
				if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
					s.Logger.Error("decoding fail", "err", err, "Transport.Addr", s.Transport.Addr())
				}

				if err := s.handleMessage(&msg, rpc.From); err != nil {
					s.Logger.Error("handleMessage fail", "err", err, "Transport.Addr", s.Transport.Addr())
				}
			}(rpc)
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

	if rc, ok := r.(io.ReadCloser); ok {
		fmt.Printf("closing read stream\n")
		defer rc.Close()
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

func (s *FileServer) handleMessageDeleteFile(msg MessageDeleteFile, from string) error {
	// peer, ok := s.peers[from]
	// if !ok {
	// 	return NotFoundPeerError{From: from}
	// }
	//TODO
	return s.store.Delete(msg.ID, msg.Key)

	// if err := peer.Send([]byte{p2p.IncomingStream}); err != nil {
	// 	return err
	// }

	// return binary.Write(peer, binary.LittleEndian, int64(1))
}

func (s *FileServer) handleMessageStoreFile(msg MessageStoreFile, from string) error {
	peer, ok := s.peers[from]
	if !ok {
		return NotFoundPeerError{From: from}
	}

	n, err := s.store.WriteDecrypt(s.EncKey, msg.ID, msg.Key, io.LimitReader(peer, msg.Size))
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes to the disk with id %s\n", s.Transport.Addr(), n, msg.ID)

	peer.CloseStream()

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

func (s *FileServer) broadcast(msg *Message) error {
	buf := new(bytes.Buffer)

	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	wg := &sync.WaitGroup{}

	for _, peer := range s.peers {
		wg.Add(1)
		go func(peer p2p.Peer) {
			defer wg.Done()
			//TODO: retry sending
			if err := peer.Send([]byte{p2p.IncomingMessage}); err != nil {
				s.Logger.Error("peer.Send IncomingMessage", "err", err)
				return
			}
			if err := peer.Send(buf.Bytes()); err != nil {
				s.Logger.Error("peer.Send msg", "err", err)
				return
			}
		}(peer)
	}

	wg.Wait()

	return nil
}

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

	msg := Message{
		Payload: MessageStoreFile{
			Key:  hashedKey,
			Size: size + 16,
			ID:   s.ID,
		},
	}

	if err := s.broadcast(&msg); err != nil {
		return err
	}

	time.Sleep(time.Millisecond * 100)

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

	if err := s.broadcast(msg); err != nil {
		return 0, nil, err
	}

	time.Sleep(time.Millisecond * 500)

	for _, peer := range s.peers {
		var fileSize int64
		if err := binary.Read(peer, binary.LittleEndian, &fileSize); err != nil {
			return 0, nil, err
		}

		n, err := s.store.WriteDecrypt(s.EncKey, s.ID, hashedKey, io.LimitReader(peer, fileSize))
		if err != nil {
			return 0, nil, err
		}

		fmt.Printf("[%s] received (%d) bytes over the network %s\n", s.Transport.Addr(), n, peer.RemoteAddr())
		peer.CloseStream()
	}

	return s.store.Read(s.ID, hashedKey)
}

func (s *FileServer) Delete(key string) error {
	hashedKey := hashKey(key)

	if err := s.store.Delete(s.ID, hashedKey); err != nil {
		return err
	}

	msg := &Message{
		Payload: MessageDeleteFile{
			Key: hashedKey,
			ID:  s.ID,
		},
	}

	if err := s.broadcast(msg); err != nil {
		return err
	}
	//TODO

	// time.Sleep(time.Millisecond * 1500)

	// for _, peer := range s.peers {
	// 	var res int64
	// 	if err := binary.Read(peer, binary.LittleEndian, &res); err != nil {
	// 		return err
	// 	}
	// 	fmt.Printf("[%s] deleted (%s) key over the network\n", s.Transport.Addr(), key)
	// }

	return nil
}

func init() {
	gob.Register(MessageStoreFile{})
	gob.Register(MessageGetFile{})
	gob.Register(MessageDeleteFile{})
}
