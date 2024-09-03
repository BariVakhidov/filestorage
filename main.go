package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/BariVakhidov/filestorage/p2p"
)

func makeServer(listenAddr string, encKey []byte, nodes ...string) (*FileServer, error) {
	tcpOpts := p2p.TCPTransportOptions{
		ListenAddr:    listenAddr,
		Decoder:       p2p.DefaultDecoder{},
		HandshakeFunc: p2p.NOPHandshakeFunc,
	}
	tcpTransport := p2p.NewTCPTransport(tcpOpts)
	//TODO: env
	logger := setupLogger(envLocal)

	serverOpts := FileServerOptions{
		StorageRoot:       listenAddr + "_network",
		Transport:         tcpTransport,
		PathTransformFunc: CASPathTransformFunc,
		BootstrapNodes:    nodes,
		EncKey:            encKey,
		Logger:            logger,
	}

	server, err := NewFileServer(serverOpts)
	if err != nil {
		return nil, err
	}

	tcpTransport.OnPeer = server.OnPeer

	return server, nil
}

func main() {
	encKey, _ := newEncryptionKey()

	s1, _ := makeServer(":3000", encKey, "")
	s2, _ := makeServer(":4000", encKey, ":3000")
	s3, _ := makeServer(":5001", encKey, ":3000", ":4000")

	go func() {
		log.Fatal(s1.Start())
	}()
	time.Sleep(time.Millisecond * 500)

	go func() {
		log.Fatal(s2.Start())
	}()

	time.Sleep(time.Millisecond * 500)

	go func() {
		log.Fatal(s3.Start())
	}()

	time.Sleep(time.Millisecond * 500)

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key_%d.png", i)
		d := bytes.NewReader([]byte(fmt.Sprintf("payload_%d", i)))
		if err := s3.Store(key, d); err != nil {
			log.Println("store err: ", err)
		}

		if err := s3.store.Delete(s3.ID, key); err != nil {
			log.Fatal(err)
		}

		_, r, err := s3.Get(key)
		if err != nil {
			log.Fatal(err)
		}

		b, err := io.ReadAll(r)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println(string(b))
	}
}
