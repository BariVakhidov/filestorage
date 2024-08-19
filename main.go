package main

import (
	"fmt"
	"log"

	"github.com/BariVakhidov/filestorage/p2p"
)

func OnPeer(p p2p.Peer) error {
	fmt.Printf("Some onPeer logic %+v\n", p)
	return nil
}

func main() {
	tcpOpts := p2p.TCPTransportOptions{
		ListenAddr:    ":3000",
		Decoder:       p2p.DefaultDecoder{},
		HandshakeFunc: p2p.NOPHandshakeFunc,
		OnPeer:        OnPeer,
	}

	tr := p2p.NewTCPTransport(tcpOpts)

	go func() {
		for {
			msg := <-tr.Consume()
			fmt.Printf("%+v\n", msg)
		}
	}()

	if err := tr.ListenAndAccept(); err != nil {
		log.Fatalln(err)
	}

	select {}
}
