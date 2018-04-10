package testing

import (
	"fmt"
	"os"
	"time"

	"github.com/fsouza/go-dockerclient"
	"gopkg.in/check.v1"
)

const primaryImage = "tsuru/base-platform"

func SetupPrimaryContainer(c *check.C) (string, func(), error) {
	dClient, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		return "", nil, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "", nil, fmt.Errorf("error getting hostname: %v", err)
	}

	err = dClient.PullImage(docker.PullImageOptions{Repository: primaryImage}, docker.AuthConfiguration{})
	if err != nil {
		return "", nil, fmt.Errorf("error pulling image %v: %v", primaryImage, err)
	}

	pCont, err := dClient.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image: primaryImage,
			Cmd:   []string{"/bin/sh", "-lc", "while true; do sleep 10; done"},
			Labels: map[string]string{
				"io.kubernetes.container.name": hostname,
				"io.kubernetes.pod.name":       hostname,
			},
		},
	})

	if err != nil {
		return "", nil, fmt.Errorf("error creating primary container: %v", err)
	}

	cleanup := func() {
		dClient.RemoveContainer(docker.RemoveContainerOptions{ID: pCont.ID, Force: true})
	}

	err = dClient.StartContainer(pCont.ID, nil)
	if err != nil {
		return pCont.ID, cleanup, fmt.Errorf("error starting primary container: %v", err)
	}

	timeout := time.After(time.Second * 15)
	for {
		c, err := dClient.InspectContainer(pCont.ID)
		if err != nil {
			return pCont.ID, cleanup, fmt.Errorf("error inspecting primary container: %v", err)
		}
		if c.State.Running {
			break
		}
		select {
		case <-time.After(time.Second):
		case <-timeout:
			return pCont.ID, cleanup, fmt.Errorf("timeout waiting for primary container to run")
		}
	}

	return pCont.ID, cleanup, nil
}
