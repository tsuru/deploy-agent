// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fsouza/go-dockerclient"
	"github.com/tsuru/tsuru/exec"
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

func (d *Executor) ExecuteAsUser(user string, opts exec.ExecuteOptions) error {
	cmd := append([]string{opts.Cmd}, opts.Args...)
	binary := filepath.Base(cmd[0])
	if binary != "bash" && binary != "sh" {
		cmd = append([]string{"/bin/sh", "-lc"}, strings.Join(cmd, " "))
	}
	if opts.Dir != "" {
		cmd = append(cmd[:2], fmt.Sprintf("cd %s && %s", opts.Dir, strings.Join(cmd[2:], " ")))
	}
	if len(opts.Envs) > 0 {
		var exports []string
		for _, e := range opts.Envs {
			exports = append(exports, fmt.Sprintf("export %s && ", e))
		}
		cmd = append(cmd[:2], fmt.Sprintf("%s %s", strings.Join(exports, ""), strings.Join(cmd[2:], " ")))
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
	execData, err := d.Client.api.InspectExec(e.ID)
	if err != nil {
		return err
	}
	if execData.ExitCode != 0 {
		return fmt.Errorf("unexpected exit code %#+v while running %v", execData.ExitCode, cmd)
	}
	return nil
}
