// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"errors"
	"fmt"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/tsuru/tsuru/exec"
)

const (
	timeoutExecWait = time.Minute
)

// Executor uses docker exec to execute a command in a running docker container
type Executor struct {
	ContainerID string
	Client      *Client
	DefaultUser string
}

func (d *Executor) Execute(opts exec.ExecuteOptions) error {
	return d.ExecuteAsUser(d.DefaultUser, opts)
}

func (e *Executor) IsRemote() bool {
	return true
}

func (d *Executor) ExecuteAsUser(user string, opts exec.ExecuteOptions) error {
	cmd := append([]string{opts.Cmd}, opts.Args...)
	if opts.Dir != "" {
		cmd = append([]string{
			"/bin/sh", "-lc",
			fmt.Sprintf("cd %s && exec $0 \"$@\"", opts.Dir),
		}, cmd...)
	}
	if len(opts.Envs) > 0 {
		envCmd := []string{"env"}
		for _, e := range opts.Envs {
			envCmd = append(envCmd, e)
		}
		cmd = append(envCmd, cmd...)
	}
	e, err := d.Client.api.CreateExec(docker.CreateExecOptions{
		Container:    d.ContainerID,
		Cmd:          cmd,
		AttachStdin:  opts.Stdin != nil,
		AttachStdout: opts.Stdout != nil,
		AttachStderr: opts.Stderr != nil,
		User:         user,
	})
	if err != nil {
		return err
	}
	err = d.Client.api.StartExec(e.ID, docker.StartExecOptions{
		OutputStream: opts.Stdout,
		InputStream:  opts.Stdin,
		ErrorStream:  opts.Stderr,
	})
	if err != nil {
		return err
	}
	exitCode, err := waitExecStatus(d.Client.api, e.ID)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("unexpected exit code %#+v while running %v", exitCode, cmd)
	}
	return nil
}

func waitExecStatus(client *docker.Client, execID string) (int, error) {
	timeout := time.After(timeoutExecWait)
	for {
		execData, err := client.InspectExec(execID)
		if err != nil {
			return 0, err
		}
		if !execData.Running {
			return execData.ExitCode, nil
		}
		select {
		case <-timeout:
			return 0, errors.New("timeout waiting for exec to finish")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
