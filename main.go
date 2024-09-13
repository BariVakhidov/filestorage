package main

import (
	"log"
	"os"

	"github.com/BariVakhidov/filestorage/p2p"
	"github.com/joho/godotenv"
)

const (
	envLocal = "local"
	envDev   = "development"
	envProd  = "production"
)

func makeServer(env string, listenAddr string, encKey []byte, nodes ...string) (*FileServer, error) {
	logger := setupLogger(env)

	tcpOpts := p2p.TCPTransportOptions{
		ListenAddr:    listenAddr,
		Decoder:       p2p.DefaultDecoder{},
		HandshakeFunc: p2p.NOPHandshakeFunc,
		Logger:        logger,
	}
	tcpTransport, err := p2p.NewTCPTransport(tcpOpts)
	if err != nil {
		return nil, err
	}

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
	tcpTransport.OnReconnect = server.OnReconnect

	return server, nil
}

func main() {
	env := os.Getenv("GO_ENV")
	if env == "" {
		env = envLocal
	}

	if env != envProd {
		err := godotenv.Load()
		if err != nil {
			log.Println("Error loading .env file")
		}
	}

	//TODO: distribute key across nodes
	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i)
	}

	portString := os.Getenv("PORT")

	if portString == "" {
		log.Fatal("No PORT provided")
	}

	server, _ := makeServer(env, portString, encKey)
	if err := server.Start(); err != nil {
		log.Fatalln(err)
	}
}
