// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/tsuru/tsuru/exec"

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

// executorFS is a filesystem backed by an executor
type executorFS struct {
	executor exec.Executor
}

func (f *executorFS) ReadFile(name string) ([]byte, error) {
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	opts := exec.ExecuteOptions{
		Cmd:    "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf("cat %s", name)},
		Stdout: out,
		Stderr: errOut,
	}
	if err := f.executor.Execute(opts); err != nil {
		return nil, fmt.Errorf("error reading file %v: %v. output: %v", name, err, errOut.String())
	}
	return out.Bytes(), nil
}

func (f *executorFS) CheckFile(name string) (bool, error) {
	outErr := new(bytes.Buffer)
	opts := exec.ExecuteOptions{
		Cmd:    "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf("stat %s", name)},
		Stderr: outErr,
	}
	if err := f.executor.Execute(opts); err != nil {
		errOut := outErr.String()
		if strings.Contains(strings.ToLower(errOut), "no such") {
			return false, nil
		}
		return false, fmt.Errorf("error checking file %v: %v. Output: %v", name, err, errOut)
	}
	return true, nil
}

func (f *executorFS) RemoveFile(name string) error {
	return f.executor.Execute(exec.ExecuteOptions{
		Cmd:  "/bin/sh",
		Args: []string{"-c", fmt.Sprintf("rm %s", name)},
	})
}

func (f *executorFS) CreateDir(dirname string) error {
	return f.executor.Execute(exec.ExecuteOptions{
		Cmd:  "/bin/sh",
		Args: []string{"-c", fmt.Sprintf("mkdir -p %s", dirname)},
	})
}
