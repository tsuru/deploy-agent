// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"fmt"
	"os"

	"github.com/fsouza/go-dockerclient"
	"github.com/tsuru/tsuru/exec"
)

// Executor uses docker exec to execute a command in a running docker container
type Executor struct {
	ContainerID string
	Client      *Client
}

func (d *Executor) Execute(opts exec.ExecuteOptions) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	cmd := append([]string{opts.Cmd}, opts.Args...)
	if opts.Dir != "" {
		cmd = append([]string{"/bin/sh", "-c", "cd", opts.Dir, "&&"}, cmd...)
	}
	if len(opts.Envs) > 0 {
		cmd = append([]string{"/bin/sh", "-c", "export"}, append(opts.Envs, cmd...)...)
	}
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
