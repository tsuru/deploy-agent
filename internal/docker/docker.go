// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
)

const (
	defaultEndpoint         = "unix:///var/run/docker.sock"
	streamInactivityTimeout = time.Minute

	dialTimeout = 10 * time.Second
	fullTimeout = 1 * time.Minute
)

type AuthConfig docker.AuthConfiguration

type Container struct {
	ID string
}

type Image struct {
	ID         string
	registry   string
	repository string
	tag        string
}

type ImageInspect docker.Image

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
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}
	cli.WithTransport(func() *http.Transport {
		return &http.Transport{
			Dial:                dialer.Dial,
			TLSHandshakeTimeout: dialTimeout,
		}
	})
	cli.SetTimeout(fullTimeout)
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

func (c *Client) Commit(ctx context.Context, containerID, image string) (string, error) {
	img := ParseImageName(image)
	commitedImg, err := c.api.CommitContainer(docker.CommitContainerOptions{
		Container:  containerID,
		Repository: img.repository,
		Tag:        img.tag,
		Context:    ctx,
	})
	if err != nil {
		return "", err
	}
	return commitedImg.ID, err
}

// Tag tags the image given by imgID with the given imageName
func (c *Client) Tag(ctx context.Context, imgID, imageName string) (Image, error) {
	img := ParseImageName(imageName)
	img.ID = imgID
	return img, c.api.TagImage(img.ID, docker.TagImageOptions{
		Repo:    img.Name(),
		Tag:     img.tag,
		Force:   true,
		Context: ctx,
	})
}

func (c *Client) Push(ctx context.Context, authConfig AuthConfig, img Image) error {
	opts := docker.PushImageOptions{
		Name:              img.Name(),
		Tag:               img.tag,
		OutputStream:      &errorCheckWriter{W: os.Stdout},
		Context:           ctx,
		InactivityTimeout: streamInactivityTimeout,
		RawJSONStream:     true,
	}
	return c.api.PushImage(opts, docker.AuthConfiguration(authConfig))
}

func (c *Client) Upload(ctx context.Context, containerID, path string, inputStream io.Reader) error {
	opts := docker.UploadToContainerOptions{
		Path:        path,
		InputStream: inputStream,
	}
	return c.api.UploadToContainer(containerID, opts)
}

func (c *Client) Inspect(ctx context.Context, img string) (ImageInspect, error) {
	inspect, err := c.api.InspectImage(img)
	if err != nil {
		return ImageInspect{}, err
	}
	return ImageInspect(*inspect), err
}

func (c *Client) BuildImage(ctx context.Context, imageName string, inputFile io.Reader) error {
	buildOptions := docker.BuildImageOptions{
		Name:              imageName,
		Pull:              true,
		NoCache:           true,
		RmTmpContainer:    true,
		InputStream:       inputFile,
		OutputStream:      &errorCheckWriter{W: os.Stdout},
		Context:           ctx,
		InactivityTimeout: streamInactivityTimeout,
		RawJSONStream:     true,
	}
	return c.api.BuildImage(buildOptions)
}

func ParseImageName(imageName string) Image {
	registry, repo, tag := splitImageName(imageName)
	return Image{
		registry:   registry,
		repository: repo,
		tag:        tag,
	}
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
