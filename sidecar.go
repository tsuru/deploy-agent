package main

import (
	"github.com/tsuru/tsuru/exec"
)

const dockerBinary = "/usr/local/bin/docker"

// dockerExecutor uses docker exec to execute a command in a running docker container
type dockerExecutor struct {
	containerID string
	executor    exec.Executor
}

func (d *dockerExecutor) Execute(opts exec.ExecuteOptions) error {
	if d.executor == nil {
		d.executor = executor()
	}
	opts.Args = append([]string{"exec", "-t", d.containerID, opts.Cmd}, opts.Args...)
	opts.Cmd = dockerBinary
	return d.executor.Execute(opts)
}
