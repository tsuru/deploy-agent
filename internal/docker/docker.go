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

	docker "github.com/fsouza/go-dockerclient"
)

const (
	defaultEndpoint         = "unix:///var/run/docker.sock"
	streamInactivityTimeout = time.Minute

	dialTimeout = 10 * time.Second
	fullTimeout = 10 * time.Minute
)

type image struct {
	id         string
	registry   string
	repository string
	tag        string
}

func (i image) name() string {
	return i.registry + "/" + i.repository
}

func (i image) String() string {
	return i.registry + "/" + i.repository + ":" + i.tag
}

type client struct {
	api *docker.Client
}

func newClient(endpoint string) (*client, error) {
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
	err = cli.Ping()
	if err != nil {
		return nil, err
	}
	return &client{
		api: cli,
	}, nil
}

func (c *client) listContainersByLabels(ctx context.Context, labels map[string]string) ([]docker.APIContainers, error) {
	filters := make(map[string][]string)
	for k, v := range labels {
		filters["label"] = append(filters["label"], fmt.Sprintf("%s=%s", k, v))
	}
	return c.api.ListContainers(docker.ListContainersOptions{Filters: filters, Context: ctx})
}

func (c *client) commit(ctx context.Context, containerID, image string) (string, error) {
	img := parseImageName(image)
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

// tag tags the image given by imgID with the given imageName
func (c *client) tag(ctx context.Context, imgID, imageName string) (image, error) {
	img := parseImageName(imageName)
	img.id = imgID
	return img, c.api.TagImage(img.id, docker.TagImageOptions{
		Repo:    img.name(),
		Tag:     img.tag,
		Force:   true,
		Context: ctx,
	})
}

func (c *client) push(ctx context.Context, authConfig docker.AuthConfiguration, img image) error {
	opts := docker.PushImageOptions{
		Name:              img.name(),
		Tag:               img.tag,
		OutputStream:      &errorCheckWriter{W: os.Stdout},
		Context:           ctx,
		InactivityTimeout: streamInactivityTimeout,
		RawJSONStream:     true,
	}
	return c.api.PushImage(opts, authConfig)
}

func (c *client) upload(ctx context.Context, containerID, path string, inputStream io.Reader) error {
	opts := docker.UploadToContainerOptions{
		Path:        path,
		InputStream: inputStream,
	}
	return c.api.UploadToContainer(containerID, opts)
}

func (c *client) inspect(ctx context.Context, img string) (*docker.Image, error) {
	return c.api.InspectImage(img)
}

func (c *client) buildImage(ctx context.Context, imageName string, inputFile io.Reader, output io.Writer) error {
	buildOptions := docker.BuildImageOptions{
		Name:              imageName,
		Pull:              true,
		NoCache:           true,
		RmTmpContainer:    true,
		InputStream:       inputFile,
		OutputStream:      &errorCheckWriter{W: output},
		Context:           ctx,
		InactivityTimeout: streamInactivityTimeout,
		RawJSONStream:     true,
	}
	return c.api.BuildImage(buildOptions)
}

func parseImageName(imageName string) image {
	registry, repo, tag := splitImageName(imageName)
	return image{
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
