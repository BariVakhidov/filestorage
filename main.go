package main

import (
	"fmt"
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
	env := os.Getenv("GO_ENV")
	if env == "" {
		env = envDev
	}

	if env != envProd {
		err := godotenv.Load()
		if err != nil {
			log.Println("Error loading .env file")
		}
	}

	encKey, _ := newEncryptionKey()
	//FIXME
	fmt.Println(encKey)
	portString := os.Getenv("PORT")

	if portString == "" {
		log.Fatal("No PORT provided")
	}
	server, err := makeServer(portString, encKey)

	if err != nil {
		log.Fatalln("Unable to start server: ", err)
	}

	go func() {
		log.Fatalln(server.Start())
	}()

	select {}
}
