package main

import (
	"bytes"
	"testing"
)

func TestCopyEncryptDecrypt(t *testing.T) {
	payload := "TEST PAYLOAD"
	src := bytes.NewReader([]byte(payload))
	dst := new(bytes.Buffer)

	key, err := newEncryptionKey()
	if err != nil {
		t.Error(err)
	}

	nw, err := copyEncrypt(key, src, dst)
	if err != nil {
		t.Error(err)
	}

	if nw != len(payload)+16 {
		t.Fail()
	}

	out := new(bytes.Buffer)
	n, err := copyDecrypt(key, dst, out)
	if err != nil {
		t.Error(err)
	}
	if n != len(payload)+16 {
		t.Fail()
	}

	if out.String() != payload {
		t.Errorf("Decryption failed!")
	}
}
