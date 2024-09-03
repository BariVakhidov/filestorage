package main

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServer(t *testing.T) {
	encKey, _ := newEncryptionKey()

	s1, _ := makeServer(":3000", encKey, "")
	s2, _ := makeServer(":4000", encKey, ":3000")
	s3, _ := makeServer(":5001", encKey, ":3000", ":4000")

	go func() {
		t.Error(s1.Start())
	}()
	time.Sleep(time.Millisecond * 500)

	go func() {
		t.Error(s2.Start())
	}()

	time.Sleep(time.Millisecond * 500)

	go func() {
		t.Error(s3.Start())
	}()

	time.Sleep(time.Millisecond * 500)

	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key_%d.png", i)
		payload := fmt.Sprintf("payload_%d", i)
		d := bytes.NewReader([]byte(payload))
		if err := s3.Store(key, d); err != nil {
			t.Error("store err: ", err)
		}

		if err := s3.store.Delete(s3.ID, key); err != nil {
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
	}
}