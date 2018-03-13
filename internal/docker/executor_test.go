// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"bytes"

	"github.com/fsouza/go-dockerclient"

	"github.com/fsouza/go-dockerclient/testing"
	"github.com/tsuru/tsuru/exec"
	"gopkg.in/check.v1"
)

func (s *S) TestExecutor(c *check.C) {
	server, err := testing.NewServer("127.0.0.1:0", nil, nil)
	c.Assert(err, check.IsNil)
	defer server.Stop()
	client, err := NewClient(server.URL())
	c.Assert(err, check.IsNil)

	err = client.api.PullImage(docker.PullImageOptions{Repository: "my-img"}, docker.AuthConfiguration{})
	c.Assert(err, check.IsNil)
	cont, err := client.api.CreateContainer(docker.CreateContainerOptions{
		Name:   "my-container",
		Config: &docker.Config{Image: "my-img"},
	})
	c.Assert(err, check.IsNil)
	err = client.api.StartContainer(cont.ID, nil)
	c.Assert(err, check.IsNil)

	var executed bool
	server.PrepareExec("*", func() {
		executed = true
	})

	e := Executor{ContainerID: cont.ID, Client: client}
	out := new(bytes.Buffer)
	err = e.Execute(exec.ExecuteOptions{
		Dir:    "/home/",
		Cmd:    "/bin/ps",
		Args:   []string{"aux"},
		Envs:   []string{"A=B"},
		Stdout: out,
	})
	c.Assert(err, check.IsNil)
	c.Assert(executed, check.Equals, true)
}
