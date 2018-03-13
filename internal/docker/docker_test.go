// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package docker

import (
	"testing"

	"github.com/fsouza/go-dockerclient"

	dockertest "github.com/fsouza/go-dockerclient/testing"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type S struct{}

var _ = check.Suite(&S{})

func (s *S) TestGetContainersByLabel(c *check.C) {
	server, err := dockertest.NewServer("127.0.0.1:0", nil, nil)
	c.Assert(err, check.IsNil)
	defer server.Stop()

	client, err := NewClient(server.URL())
	c.Assert(err, check.IsNil)
	err = client.api.PullImage(docker.PullImageOptions{Repository: "my-img"}, docker.AuthConfiguration{})
	c.Assert(err, check.IsNil)
	cont, err := client.api.CreateContainer(docker.CreateContainerOptions{
		Name: "my-cont",
		Config: &docker.Config{
			Image: "my-img",
			Labels: map[string]string{
				"A": "VA",
				"B": "VB",
			},
		},
	})
	c.Assert(err, check.IsNil)
	err = client.api.StartContainer(cont.ID, nil)
	c.Assert(err, check.IsNil)
	cont2, err := client.api.CreateContainer(docker.CreateContainerOptions{
		Name:   "my-cont2",
		Config: &docker.Config{Image: "my-img"},
	})
	c.Assert(err, check.IsNil)
	err = client.api.StartContainer(cont2.ID, nil)
	c.Assert(err, check.IsNil)

	containers, err := client.ListContainersByLabels(map[string]string{"A": "VA", "B": "VB"})
	c.Assert(err, check.IsNil)
	c.Assert(containers, check.DeepEquals, []Container{{ID: cont.ID}})
}
