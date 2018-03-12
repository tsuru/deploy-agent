// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"testing"

	"github.com/tsuru/tsuru/exec/exectest"
	"github.com/tsuru/tsuru/fs/fstest"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type S struct {
	fs   *localFS
	exec *exectest.FakeExecutor
}

var _ = check.Suite(&S{})

func (s *S) SetUpTest(c *check.C) {
	s.fs = &localFS{Fs: &fstest.RecordingFs{}}
	s.exec = &exectest.FakeExecutor{}
	err := s.fs.Mkdir(defaultWorkingDir, 0777)
	c.Assert(err, check.IsNil)
}

func (s *S) testFS() *fstest.RecordingFs {
	return s.fs.Fs.(*fstest.RecordingFs)
}
