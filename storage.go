package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

const defaultRootPath = "bvnetwork"

type PathKey struct {
	Filename string
	Pathname string
}

func (p PathKey) FullPath() string {
	return fmt.Sprintf("%s/%s", p.Pathname, p.Filename)
}

func (p PathKey) FirstPathname() string {
	paths := strings.Split(p.Pathname, "/")
	if len(paths) == 0 {
		return ""
	}

	return paths[0]
}

func CASPathTransformFunc(key string) PathKey {
	hash := sha1.Sum([]byte(key))
	hashStr := hex.EncodeToString(hash[:])

	blockSize := 5
	sliceLength := len(hashStr) / blockSize
	paths := make([]string, sliceLength)

	for i := 0; i < sliceLength; i++ {
		from, to := i*blockSize, (i*blockSize)+blockSize
		paths[i] = hashStr[from:to]
	}

	return PathKey{
		Pathname: strings.Join(paths, "/"),
		Filename: hashStr,
	}
}

type PathTransformFunc func(string) PathKey

func DefaultTransformFunc(key string) PathKey {
	return PathKey{
		Pathname: key,
		Filename: key,
	}
}

type StorageOptions struct {
	//Root is the folder name of the root, containing all the folders of the system
	Root              string
	PathTransformFunc PathTransformFunc
}

type Storage struct{ StorageOptions }

func NewStorage(opts StorageOptions) *Storage {
	if opts.PathTransformFunc == nil {
		opts.PathTransformFunc = DefaultTransformFunc
	}

	if len(opts.Root) == 0 {
		opts.Root = defaultRootPath
	}

	return &Storage{
		StorageOptions: opts,
	}
}

func (s *Storage) Read(id, key string) (int64, io.Reader, error) {
	return s.readStream(id, key)
}

func (s *Storage) Write(id, key string, r io.Reader) (int64, error) {
	return s.writeStream(id, key, r)
}

func (s *Storage) openFileForWriting(id, key string) (*os.File, error) {
	pathKey := s.PathTransformFunc(key)
	pathKeyWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.Pathname)

	if err := os.MkdirAll(pathKeyWithRoot, os.ModePerm); err != nil {
		return nil, err
	}

	fullPathWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FullPath())

	return os.Create(fullPathWithRoot)
}

func (s *Storage) readStream(id, key string) (int64, io.ReadCloser, error) {
	pathKey := s.PathTransformFunc(key)
	fullPathWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FullPath())
	file, err := os.Open(fullPathWithRoot)

	if err != nil {
		return 0, nil, err
	}

	fi, err := file.Stat()
	if err != nil {
		return 0, nil, err
	}

	return fi.Size(), file, nil
}

func (s *Storage) WriteDecrypt(encKey []byte, id, key string, r io.Reader) (int64, error) {
	file, err := s.openFileForWriting(id, key)

	if err != nil {
		return 0, err
	}

	n, err := copyDecrypt(encKey, r, file)
	return int64(n), err
}

func (s *Storage) writeStream(id, key string, r io.Reader) (int64, error) {
	file, err := s.openFileForWriting(id, key)

	if err != nil {
		return 0, err
	}

	return io.Copy(file, r)
}

func (s *Storage) Delete(id, key string) error {
	pathKey := s.PathTransformFunc(key)

	defer func() {
		fmt.Printf("deleted [%s] from the disk\n", pathKey.Filename)
	}()

	firstPathnameWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FirstPathname())

	return os.RemoveAll(firstPathnameWithRoot)
}

func (s *Storage) Has(id, key string) bool {
	pathKey := s.PathTransformFunc(key)
	fullPathWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FullPath())

	_, err := os.Stat(fullPathWithRoot)

	return !errors.Is(err, fs.ErrNotExist)
}

func (s *Storage) Clear() error {
	return os.RemoveAll(s.Root)
}
