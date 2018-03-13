// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/exec/exectest"
	"gopkg.in/check.v1"
)

func (s *S) TestExecutor(c *check.C) {
	fake := &exectest.FakeExecutor{}
	e := Executor{containerID: "dd5e0fbf6d3c", executor: fake}
	e.Execute(exec.ExecuteOptions{
		Cmd: "/bin/ps",
	})
	executedCmds := fake.GetCommands(dockerBinary)
	c.Assert(len(executedCmds), check.Equals, 1)
	args := executedCmds[0].GetArgs()
	expectedArgs := []string{"exec", "-t", "dd5e0fbf6d3c", "/bin/ps"}
	c.Assert(args, check.DeepEquals, expectedArgs)
}
