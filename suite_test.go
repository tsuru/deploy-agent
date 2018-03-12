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
	fs   *fstest.RecordingFs
	exec *exectest.FakeExecutor
}

var _ = check.Suite(&S{})

func (s *S) SetUpTest(c *check.C) {
	s.fs = &fstest.RecordingFs{}
	s.exec = &exectest.FakeExecutor{}
	osExecutor = s.exec
	err := s.fs.Mkdir(defaultWorkingDir, 0777)
	c.Assert(err, check.IsNil)
}
