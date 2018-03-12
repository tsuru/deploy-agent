package main

import (
	"github.com/tsuru/tsuru/exec"
	"gopkg.in/check.v1"
)

func (s *S) TestDockerExecutor(c *check.C) {
	e := dockerExecutor{containerID: "dd5e0fbf6d3c"}
	e.Execute(exec.ExecuteOptions{
		Cmd: "/bin/ps",
	})
	executedCmds := s.exec.GetCommands(dockerBinary)
	c.Assert(len(executedCmds), check.Equals, 1)
	args := executedCmds[0].GetArgs()
	expectedArgs := []string{"exec", "-t", "dd5e0fbf6d3c", "/bin/ps"}
	c.Assert(args, check.DeepEquals, expectedArgs)
}
