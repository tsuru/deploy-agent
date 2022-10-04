// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"os"

	"github.com/tsuru/tsuru/exec"

	"gopkg.in/check.v1"
)

type FilesystemSuite struct{}

var _ = check.Suite(&FilesystemSuite{})

func (s *FilesystemSuite) TestExecutorFSReadFile(c *check.C) {
	tmpFile, err := os.CreateTemp("", "")
	c.Assert(err, check.IsNil)
	defer os.Remove(tmpFile.Name())
	err = os.WriteFile(tmpFile.Name(), []byte("this is the file content"), 0666)
	c.Assert(err, check.IsNil)
	fs := executorFS{executor: &exec.OsExecutor{}}
	data, err := fs.ReadFile(tmpFile.Name())
	c.Assert(err, check.IsNil)
	c.Assert(data, check.DeepEquals, []byte("this is the file content"))
}

func (s *FilesystemSuite) TestExecutorFSCheckFile(c *check.C) {
	tmpFile, err := os.CreateTemp("", "")
	c.Assert(err, check.IsNil)
	defer os.Remove(tmpFile.Name())
	fs := executorFS{executor: &exec.OsExecutor{}}
	exists, err := fs.CheckFile(tmpFile.Name())
	c.Assert(err, check.IsNil)
	c.Assert(exists, check.Equals, true)
	os.Remove(tmpFile.Name())
	exists, err = fs.CheckFile(tmpFile.Name())
	c.Assert(err, check.IsNil)
	c.Assert(exists, check.Equals, false)
}

func (s *FilesystemSuite) TestExecutorFSRemoveFile(c *check.C) {
	tmpFile, err := os.CreateTemp("", "")
	c.Assert(err, check.IsNil)
	defer os.Remove(tmpFile.Name())
	fs := executorFS{executor: &exec.OsExecutor{}}
	err = fs.RemoveFile(tmpFile.Name())
	c.Assert(err, check.IsNil)
	_, err = os.Stat(tmpFile.Name())
	c.Assert(os.IsNotExist(err), check.Equals, true)
}
