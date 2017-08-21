// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package user

import (
	"os/user"
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

func (s *S) TestChangeUserWithAnotherUID(c *check.C) {
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
usermod -u 1234 .+?;
groupmod -g 1234 .+?;
useradd -M -U -u \d+ tsuru\.old\..+?;
echo "tsuru\.old\..+? ALL=\(#1234\) NOPASSWD:ALL" >>/etc/sudoers;
find / -mount -user \d+ -exec chown -h 1234:1234 \{\} \+;
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
	u, err := user.Current()
	c.Assert(err, check.IsNil)
	executor, err := ChangeUser(s.exec, []bind.EnvVar{
		{Name: "TSURU_OS_UID", Value: u.Uid},
	})
	c.Assert(err, check.IsNil)
	c.Assert(executor, check.Equals, s.exec)
	cmds := s.exec.GetCommands("sudo")
	c.Assert(cmds, check.HasLen, 0)
}

func (s *S) TestChangeUserWithOldUser(c *check.C) {
	testProcessCurrentUser = func(u *user.User) {
		u.Name = "tsuru.old.myuser"
		u.Username = u.Name
	}
	defer func() { testProcessCurrentUser = nil }()
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
		"-u", "#999", "--", "mycmd", "arg1", "arg2",
	})
}
