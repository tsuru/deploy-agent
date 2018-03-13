// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"fmt"

	"github.com/fsouza/go-dockerclient"
)

const defaultEndpoint = "unix:///var/run/docker.sock"

type Container struct {
	ID string
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

func (c *Client) ListContainersByLabels(labels map[string]string) ([]Container, error) {
	filters := make(map[string][]string)
	for k, v := range labels {
		filters["label"] = append(filters["label"], fmt.Sprintf("%s=%s", k, v))
	}
	containers, err := c.api.ListContainers(docker.ListContainersOptions{Filters: filters})
	if err != nil {
		return nil, err
	}
	var conts []Container
	for _, c := range containers {
		conts = append(conts, Container{ID: c.ID})
	}
	return conts, err
}
