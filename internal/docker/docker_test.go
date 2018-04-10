// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/fsouza/go-dockerclient"

	dockertest "github.com/fsouza/go-dockerclient/testing"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type S struct {
	dockerserver *dockertest.DockerServer
}

var _ = check.Suite(&S{})

func (s *S) SetUpTest(c *check.C) {
	server, err := dockertest.NewServer("127.0.0.1:0", nil, nil)
	c.Assert(err, check.IsNil)
	s.dockerserver = server
}

func (s *S) TeardownTest(c *check.C) {
	s.dockerserver.Stop()
}

func (s *S) addTestContainer(c *check.C, name, image string) string {
	client, err := NewClient(s.dockerserver.URL())
	c.Assert(err, check.IsNil)
	err = client.api.PullImage(docker.PullImageOptions{Repository: image}, docker.AuthConfiguration{})
	c.Assert(err, check.IsNil)
	cont, err := client.api.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image: image,
			Labels: map[string]string{
				"A": "VA",
				"B": "VB",
			},
		},
	})
	c.Assert(err, check.IsNil)
	err = client.api.StartContainer(cont.ID, nil)
	c.Assert(err, check.IsNil)
	return cont.ID
}

func (s *S) TestGetContainersByLabel(c *check.C) {
	client, err := NewClient(s.dockerserver.URL())
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

	containers, err := client.ListContainersByLabels(context.Background(), map[string]string{"A": "VA", "B": "VB"})
	c.Assert(err, check.IsNil)
	c.Assert(containers, check.DeepEquals, []Container{{ID: cont.ID}})
}

func (s *S) TestSplitImageName(c *check.C) {
	tt := []struct {
		image  string
		exReg  string
		exRepo string
		exTag  string
	}{
		{
			image:  "10.200.10.1:5000/admin/app-myapp:v23-builder",
			exReg:  "10.200.10.1:5000",
			exRepo: "admin/app-myapp",
			exTag:  "v23-builder",
		},
		{
			image:  "10.200.10.1:5000/admin/app-myapp",
			exReg:  "10.200.10.1:5000",
			exRepo: "admin/app-myapp",
			exTag:  "latest",
		},
		{
			image:  "myregistry.com/admin/app-myapp",
			exReg:  "myregistry.com",
			exRepo: "admin/app-myapp",
			exTag:  "latest",
		},
	}
	for _, t := range tt {
		reg, repo, tag := splitImageName(t.image)
		c.Assert(reg, check.Equals, t.exReg, check.Commentf("image: %s", t.image))
		c.Assert(repo, check.Equals, t.exRepo, check.Commentf("image: %s", t.image))
		c.Assert(tag, check.Equals, t.exTag, check.Commentf("image: %s", t.image))
	}
}

func (s *S) TestClientCommit(c *check.C) {
	client, err := NewClient(s.dockerserver.URL())
	c.Assert(err, check.IsNil)
	contID := s.addTestContainer(c, "my-cont", "my-img")

	img, err := client.Commit(context.Background(), contID, "10.200.10.1:5000/admin/app-myapp:v23-builder")
	c.Assert(err, check.IsNil)
	dockerImage, err := client.api.InspectImage("admin/app-myapp:v23-builder")
	c.Assert(err, check.IsNil)
	c.Assert(img, check.DeepEquals, dockerImage.ID)
}

func (s *S) TestClientTag(c *check.C) {
	client, err := NewClient(s.dockerserver.URL())
	c.Assert(err, check.IsNil)
	contID := s.addTestContainer(c, "my-cont", "my-img")

	imgID, err := client.Commit(context.Background(), contID, "10.200.10.1:5000/admin/app-myapp:v23-builder")
	c.Assert(err, check.IsNil)
	img, err := client.Tag(context.Background(), imgID, "10.200.10.1:5000/admin/app-myapp:v23-builder")
	c.Assert(err, check.IsNil)
	dockerImage, err := client.api.InspectImage("10.200.10.1:5000/admin/app-myapp:v23-builder")
	c.Assert(err, check.IsNil)
	c.Assert(dockerImage.ID, check.DeepEquals, img.ID)
}

func (s *S) TestClientPush(c *check.C) {
	client, err := NewClient(s.dockerserver.URL())
	c.Assert(err, check.IsNil)
	contID := s.addTestContainer(c, "my-cont", "my-img")

	imgID, err := client.Commit(context.Background(), contID, "10.200.10.1:5000/admin/app-myapp:v23-builder")
	c.Assert(err, check.IsNil)
	img, err := client.Tag(context.Background(), imgID, "10.200.10.1:5000/admin/app-myapp:v23-builder")
	c.Assert(err, check.IsNil)
	err = client.Push(context.Background(), AuthConfig{}, img)
	c.Assert(err, check.IsNil)
}

func (s *S) TestClientUpload(c *check.C) {
	client, err := NewClient(s.dockerserver.URL())
	c.Assert(err, check.IsNil)
	contID := s.addTestContainer(c, "my-cont", "my-img")

	data := bytes.NewBuffer([]byte("file content"))
	dataSize := int64(data.Len())
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	err = tw.WriteHeader(&tar.Header{
		Name: "myfile.txt",
		Mode: 0666,
		Size: dataSize,
	})
	c.Assert(err, check.IsNil)
	defer tw.Close()
	n, err := io.Copy(tw, data)
	c.Assert(err, check.IsNil)
	c.Assert(n, check.Equals, dataSize)
	err = client.Upload(context.Background(), contID, "/", buf)
	c.Assert(err, check.IsNil)
	err = client.api.DownloadFromContainer(contID, docker.DownloadFromContainerOptions{Path: "/myfile.txt"})
	c.Assert(err, check.IsNil)
}

func (s *S) TestInspect(c *check.C) {
	client, err := NewClient(s.dockerserver.URL())
	c.Assert(err, check.IsNil)

	err = client.api.PullImage(docker.PullImageOptions{Repository: "image"}, docker.AuthConfiguration{})
	c.Assert(err, check.IsNil)

	img, err := client.Inspect(context.Background(), "image")
	c.Assert(err, check.IsNil)
	c.Assert(img.ID, check.Not(check.DeepEquals), "")
}
