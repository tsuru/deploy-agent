// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"fmt"

	"github.com/fsouza/go-dockerclient"
	"github.com/tsuru/tsuru/exec"
)

// Executor uses docker exec to execute a command in a running docker container
type Executor struct {
	ContainerID string
	Client      *Client
}

func (d *Executor) Execute(opts exec.ExecuteOptions) error {
	cmd := append([]string{opts.Cmd}, opts.Args...)
	if opts.Dir != "" {
		cmd = append([]string{"cd", opts.Dir, "&&"}, cmd...)
	}
	// TODO: Envs for API pre v1.25
	e, err := d.Client.api.CreateExec(docker.CreateExecOptions{
		Container:    d.ContainerID,
		Cmd:          cmd,
		AttachStdin:  opts.Stdin != nil,
		AttachStdout: opts.Stdout != nil,
		AttachStderr: opts.Stderr != nil,
	})
	if err != nil {
		return err
	}
	err = d.Client.api.StartExec(e.ID, docker.StartExecOptions{
		OutputStream: opts.Stdout,
		InputStream:  opts.Stdin,
		ErrorStream:  opts.Stderr,
		Tty:          true,
		RawTerminal:  true,
	})
	if err != nil {
		return err
	}
	execData, err := d.Client.api.InspectExec(e.ID)
	if err != nil {
		return err
	}
	if execData.ExitCode != 0 {
		return fmt.Errorf("container exited with error: %v", execData.ExitCode)
	}
	return nil
}
