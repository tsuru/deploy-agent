// Copyright 2018 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"github.com/tsuru/tsuru/exec"
)

const dockerBinary = "/usr/local/bin/docker"

// Executor uses docker exec to execute a command in a running docker container
type Executor struct {
	containerID string
	executor    exec.Executor
}

func (d *Executor) Execute(opts exec.ExecuteOptions) error {
	// TODO: use the exec API to properly configure workdir etc...
	if d.executor == nil {
		d.executor = &exec.OsExecutor{}
	}
	opts.Args = append([]string{"exec", "-t", d.containerID, opts.Cmd}, opts.Args...)
	opts.Cmd = dockerBinary
	return d.executor.Execute(opts)
}
