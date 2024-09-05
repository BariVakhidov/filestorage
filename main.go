package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/BariVakhidov/filestorage/p2p"
	"github.com/joho/godotenv"
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
	err := godotenv.Load()
	if err != nil {
		log.Fatalln(err)
	}

	portString := os.Getenv("PORT")

	if portString == "" {
		log.Fatal("No PORT provided")
	}

	encKey, _ := newEncryptionKey()
	//FIXME
	fmt.Println(encKey)
	server, err := makeServer(portString, encKey)

	if err != nil {
		slog.Error("server error", "err", err)
		return
	}

	if err := server.Start(); err != nil {
		slog.Error("server can't start", "err", err)
		return
	}
}
