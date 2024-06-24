package main

import (
	"github.com/BariVakhidov/filestorage/p2p"
	"log"
)

func main() {
	tr := p2p.NewTCPTransport(":3000")
	err := tr.ListenAndAccept()
	if err != nil {
		log.Fatalln(err)
	}
	select {}
}
