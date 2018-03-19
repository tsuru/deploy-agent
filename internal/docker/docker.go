// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fsouza/go-dockerclient"
)

const defaultEndpoint = "unix:///var/run/docker.sock"

type Container struct {
	ID string
}

type Image struct {
	ID         string
	registry   string
	repository string
	tag        string
}

func (i Image) Name() string {
	return i.registry + "/" + i.repository
}

func (i Image) String() string {
	return i.registry + "/" + i.repository + ":" + i.tag
}

type Client struct {
	api *docker.Client
}

func NewClient(endpoint string) (*Client, error) {
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	cli, err := docker.NewClient(endpoint)
	if err != nil {
		return nil, err
	}
	return &Client{
		api: cli,
	}, nil
}

func (c *Client) ListContainersByLabels(ctx context.Context, labels map[string]string) ([]Container, error) {
	filters := make(map[string][]string)
	for k, v := range labels {
		filters["label"] = append(filters["label"], fmt.Sprintf("%s=%s", k, v))
	}
	containers, err := c.api.ListContainers(docker.ListContainersOptions{Filters: filters, Context: ctx})
	if err != nil {
		return nil, err
	}
	var conts []Container
	for _, c := range containers {
		conts = append(conts, Container{ID: c.ID})
	}
	return conts, err
}

func (c *Client) Commit(ctx context.Context, containerID, image string) (Image, error) {
	registry, repo, tag := splitImageName(image)
	img := Image{
		registry:   registry,
		repository: repo,
		tag:        tag,
	}
	commitedImg, err := c.api.CommitContainer(docker.CommitContainerOptions{
		Container:  containerID,
		Repository: img.repository,
		Tag:        img.tag,
		Context:    ctx,
	})
	if err != nil {
		return Image{}, err
	}
	img.ID = commitedImg.ID
	return img, err
}

func (c *Client) Tag(ctx context.Context, img Image) error {
	return c.api.TagImage(img.ID, docker.TagImageOptions{
		Repo:    img.Name(),
		Tag:     img.tag,
		Force:   true,
		Context: ctx,
	})
}

func (c *Client) Push(ctx context.Context, img Image) error {
	opts := docker.PushImageOptions{
		Name:          img.Name(),
		Tag:           img.tag,
		RawJSONStream: true,
		OutputStream:  os.Stdout,
		Context:       ctx,
	}
	return c.api.PushImage(opts, docker.AuthConfiguration{})
}

func splitImageName(imageName string) (registry, repo, tag string) {
	imgNameSplit := strings.Split(imageName, ":")
	switch len(imgNameSplit) {
	case 1:
		repo = imgNameSplit[0]
		tag = "latest"
	case 2:
		if strings.Contains(imgNameSplit[1], "/") {
			repo = imageName
			tag = "latest"
		} else {
			repo = imgNameSplit[0]
			tag = imgNameSplit[1]
		}
	default:
		repo = strings.Join(imgNameSplit[:len(imgNameSplit)-1], ":")
		tag = imgNameSplit[len(imgNameSplit)-1]
	}
	repoSplit := strings.SplitN(repo, "/", 2)
	registry = repoSplit[0]
	repo = repoSplit[1]
	return
}
