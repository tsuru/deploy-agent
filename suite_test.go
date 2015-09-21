package main

import (
	"github.com/tsuru/tsuru/exec/exectest"
	"github.com/tsuru/tsuru/fs/fstest"
	"gopkg.in/check.v1"
	"testing"
)

func Test(t *testing.T) { check.TestingT(t) }

type S struct {
	fs   *fstest.RecordingFs
	exec *exectest.FakeExecutor
}

var _ = check.Suite(&S{})

func (s *S) SetUpTest(c *check.C) {
	s.fs = &fstest.RecordingFs{}
	fsystem = s.fs
	s.exec = &exectest.FakeExecutor{}
	osExecutor = s.exec
	err := s.fs.Mkdir(defaultWorkingDir, 0777)
	c.Assert(err, check.IsNil)
}

func (s *S) TeardownTest(c *check.C) {
	fsystem = nil
}
