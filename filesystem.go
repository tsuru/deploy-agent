package main

import (
	"io/ioutil"
	"os"

	"github.com/tsuru/tsuru/fs"
)

type Filesystem interface {
	ReadFile(name string) ([]byte, error)
	CheckFile(name string) (bool, error)
	RemoveFile(name string) error
}

// localFS is a wrapper around fs.Fs that implements Filesystem
type localFS struct{ fs.Fs }

func (f *localFS) ReadFile(name string) ([]byte, error) {
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ioutil.ReadAll(file)
}

func (f *localFS) CheckFile(name string) (bool, error) {
	_, err := f.Fs.Stat(name)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (f *localFS) RemoveFile(name string) error {
	return f.Fs.Remove(name)
}
