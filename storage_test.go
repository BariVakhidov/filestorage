package main

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

func TestTransformFunc(t *testing.T) {
	key := "testkey"
	pathKey := CASPathTransformFunc(key)
	expectedFilename := "913a73b565c8e2c8ed94497580f619397709b8b6"
	expectedPathName := "913a7/3b565/c8e2c/8ed94/49758/0f619/39770/9b8b6"

	if pathKey.Filename != expectedFilename {
		t.Errorf("have %s want %s", pathKey.Filename, expectedFilename)
	}

	if pathKey.Pathname != expectedPathName {
		t.Errorf("have %s want %s", pathKey.Pathname, expectedPathName)
	}

	fmt.Println(pathKey)
}

func TestStorage(t *testing.T) {
	s := createStore()
	defer teardown(t, s)

	for i := 0; i < 50; i++ {
		data := []byte("some test")
		key := fmt.Sprintf("foo_%d", i)

		if err := s.Write(key, bytes.NewReader(data)); err != nil {
			t.Error(err)
		}

		if exists := s.Has(key); !exists {
			t.Errorf("expected to have key %s", key)
		}

		buf, err := s.Read(key)
		if err != nil {
			t.Error(err)
		}

		b, _ := io.ReadAll(buf)

		if string(b) != string(data) {
			t.Errorf("want %s have %s", data, b)
		}

		if err := s.Delete(key); err != nil {
			t.Error(err)
		}

		if exists := s.Has(key); exists {
			t.Errorf("expected to NOT have %s", key)
		}
	}
}

func createStore() *Storage {
	opts := StorageOptions{
		PathTransformFunc: CASPathTransformFunc,
	}

	return NewStorage(opts)
}

func teardown(t *testing.T, s *Storage) {
	if err := s.Clear(); err != nil {
		t.Error(err)
	}
}
