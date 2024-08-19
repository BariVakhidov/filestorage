package main

import (
	"io"
	"log"
	"os"
)

type PathTransformFunc func(string) string

func DefaultTransformFunc(key string) string {
	return key
}

type StorageOptions struct {
	PathTransformFunc PathTransformFunc
}

type Storage struct{ StorageOptions }

func NewStorage(opts StorageOptions) *Storage {
	return &Storage{StorageOptions: opts}
}

func (s *Storage) writeStream(key string, r io.Reader) error {
	pathName := s.PathTransformFunc(key)

	if err := os.MkdirAll(pathName, os.ModePerm); err != nil {
		return err
	}

	filename := "testFileName"
	pathAndFilename := pathName + "/" + filename

	f, err := os.Create(pathAndFilename)
	if err != nil {
		return err
	}

	n, err := io.Copy(f, r)
	if err != nil {
		return err
	}

	log.Printf("written (%d) bytes to the disk, %s\n", n, pathAndFilename)

	return nil
}
