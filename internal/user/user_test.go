// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package user

import (
	"testing"

	"github.com/tsuru/tsuru/app/bind"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/exec/exectest"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type S struct {
	exec *exectest.FakeExecutor
}

var _ = check.Suite(&S{})

func (s *S) SetUpTest(c *check.C) {
	s.exec = &exectest.FakeExecutor{}
}

type fakeAsUserExecutor struct {
	*exectest.FakeExecutor
	ranAsUser string
}

func (f *fakeAsUserExecutor) ExecuteAsUser(user string, opts exec.ExecuteOptions) error {
	f.ranAsUser = user
	return f.FakeExecutor.Execute(opts)
}

func (s *S) TestChangeUserWithAnotherUID(c *check.C) {
	s.exec.Output = map[string][][]byte{
		"-un": {
			[]byte("myubuntu"),
		},
		"-u": {
			[]byte("1009"),
		},
	}
	executor, err := ChangeUser(s.exec, []bind.EnvVar{
		{Name: "TSURU_OS_UID", Value: "1234"},
	})
	c.Assert(err, check.IsNil)
	c.Assert(executor, check.DeepEquals, &userExecutor{
		baseExecutor: s.exec,
		uid:          1234,
	})
	cmds := s.exec.GetCommands("sudo")
	c.Assert(cmds, check.HasLen, 1)
	args := cmds[0].GetArgs()
	c.Assert(args, check.HasLen, 4)
	c.Assert(args[:3], check.DeepEquals, []string{"--", "sh", "-c"})
	c.Assert(args[3], check.Matches, `(?s)
usermod -u 1234 myubuntu;
groupmod -g 1234 myubuntu;
useradd -M -U -u 1009 tsuru\.old\.myubuntu;
echo "tsuru\.old\.myubuntu ALL=\(#1234\) NOPASSWD:ALL" >>/etc/sudoers;
find / -mount -user 1009 -exec chown -h 1234:1234 \{\} \+;
`)
}

func (s *S) TestChangeUserNoEnvs(c *check.C) {
	executor, err := ChangeUser(s.exec, nil)
	c.Assert(err, check.IsNil)
	c.Assert(executor, check.Equals, s.exec)
	cmds := s.exec.GetCommands("sudo")
	c.Assert(cmds, check.HasLen, 0)
}

func (s *S) TestChangeUserSameUser(c *check.C) {
	s.exec.Output = map[string][][]byte{
		"-un": {
			[]byte("ubuntu"),
		},
		"-u": {
			[]byte("1009"),
		},
	}
	executor, err := ChangeUser(s.exec, []bind.EnvVar{
		{Name: "TSURU_OS_UID", Value: "1009"},
	})
	c.Assert(err, check.IsNil)
	c.Assert(executor, check.Equals, s.exec)
	cmds := s.exec.GetCommands("sudo")
	c.Assert(cmds, check.HasLen, 0)
}

func (s *S) TestChangeUserWithOldUser(c *check.C) {
	s.exec.Output = map[string][][]byte{
		"-un": {
			[]byte("tsuru.old.ubuntu"),
		},
		"-u": {
			[]byte("1009"),
		},
	}
	executor, err := ChangeUser(s.exec, []bind.EnvVar{
		{Name: "TSURU_OS_UID", Value: "9998"},
	})
	c.Assert(err, check.IsNil)
	c.Assert(executor, check.DeepEquals, &userExecutor{
		baseExecutor: s.exec,
		uid:          9998,
	})
	cmds := s.exec.GetCommands("sudo")
	c.Assert(cmds, check.HasLen, 0)
}

func (s *S) TestUserExecutorExecute(c *check.C) {
	exe := userExecutor{
		baseExecutor: s.exec,
		uid:          999,
	}
	err := exe.Execute(exec.ExecuteOptions{
		Cmd:  "mycmd",
		Args: []string{"arg1", "arg2"},
	})
	c.Assert(err, check.IsNil)
	cmds := s.exec.GetCommands("sudo")
	c.Assert(cmds, check.HasLen, 1)
	args := cmds[0].GetArgs()
	c.Assert(args, check.DeepEquals, []string{
		"-u", "#999", "-g", "#999", "--", "mycmd", "arg1", "arg2",
	})
}

func (s *S) TestUserExecutorExecuteAsUser(c *check.C) {
	base := fakeAsUserExecutor{FakeExecutor: s.exec}
	exe := userExecutor{
		baseExecutor: &base,
		uid:          999,
	}
	err := exe.Execute(exec.ExecuteOptions{
		Cmd:  "mycmd",
		Args: []string{"arg1", "arg2"},
	})
	c.Assert(err, check.IsNil)
	c.Assert(base.ranAsUser, check.DeepEquals, "999:999")
	cmds := s.exec.GetCommands("mycmd")
	c.Assert(cmds, check.HasLen, 1)
	args := cmds[0].GetArgs()
	c.Assert(args, check.DeepEquals, []string{"arg1", "arg2"})
}
