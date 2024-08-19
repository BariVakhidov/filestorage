package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStorage(t *testing.T) {
	opts := StorageOptions{
		PathTransformFunc: DefaultTransformFunc,
	}

	s := NewStorage(opts)
	data := bytes.NewReader([]byte("some test"))

	if err := s.writeStream("testpicture", data); err != nil {
		t.Error(err)
	}

	assert.Equal(t, s.PathTransformFunc("test"), "test")
}
