package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

func TestServer(t *testing.T) {
	encKey, _ := newEncryptionKey()
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
	s1, _ := makeServer(env, ":3000", encKey, "")
	s2, _ := makeServer(env, ":4000", encKey, ":3000")
	s3, _ := makeServer(env, ":5001", encKey, ":3000", ":4000")

	go func() {
		t.Error(s1.Start())
	}()
	time.Sleep(time.Millisecond * 100)

	go func() {
		t.Error(s2.Start())
	}()

	time.Sleep(time.Millisecond * 100)

	go func() {
		t.Error(s3.Start())
	}()

	time.Sleep(time.Millisecond * 100)
	wg := &sync.WaitGroup{}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			key := fmt.Sprintf("key_%d.png", i)
			payload := fmt.Sprintf("payload_%d", i)
			d := bytes.NewReader([]byte(payload))
			if err := s3.Store(key, d); err != nil {
				t.Error("store err: ", err)
			}

			if err := s3.store.Delete(s3.ID, hashKey(key)); err != nil {
				t.Error(err)
			}
			_, r, err := s3.Get(key)
			if err != nil {
				t.Error(err)
			}

			b, err := io.ReadAll(r)
			if err != nil {
				t.Error(err)
			}

			assert.Equal(t, payload, string(b))

			if err := s3.Delete(key); err != nil {
				t.Error(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}
