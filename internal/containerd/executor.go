package containerd

import (
	"context"
	"crypto"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/containerd/containerd/cio"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/tsuru/tsuru/exec"
)

var _ exec.Executor = &containerdExecutor{}

type containerdExecutor struct {
	sidecar *containerdSidecar
	ctx     context.Context
}

func (e *containerdExecutor) Execute(opts exec.ExecuteOptions) error {
	return e.ExecuteAsUser(e.sidecar.user, opts)
}

func (e *containerdExecutor) IsRemote() bool {
	return true
}

func (e *containerdExecutor) ExecuteAsUser(user string, opts exec.ExecuteOptions) error {
	fullCmd := append([]string{opts.Cmd}, opts.Args...)

	container, err := e.sidecar.client.LoadContainer(e.ctx, e.sidecar.primaryContainerID)
	if err != nil {
		return err
	}
	spec, err := container.Spec(e.ctx)
	if err != nil {
		return err
	}
	pspec := spec.Process
	pspec.Args = fullCmd
	if user != "" {
		pspec.User = specs.User{
			Username: user,
		}
	}
	pspec.Env = append(pspec.Env, opts.Envs...)
	if opts.Dir != "" {
		pspec.Cwd = opts.Dir
	}

	task, err := container.Task(e.ctx, nil)
	if err != nil {
		return err
	}

	if opts.Stdout == nil {
		opts.Stdout = ioutil.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = ioutil.Discard
	}
	ioCreator := cio.NewCreator(cio.WithStreams(opts.Stdin, opts.Stdout, opts.Stderr))

	execID := "exec-" + randID()
	process, err := task.Exec(e.ctx, execID, pspec, ioCreator)
	if err != nil {
		return err
	}
	defer process.Delete(e.ctx)

	statusCh, err := process.Wait(e.ctx)
	if err != nil {
		return err
	}

	err = process.Start(e.ctx)
	if err != nil {
		return err
	}

	select {
	case status := <-statusCh:
		if status.ExitCode() != 0 {
			return fmt.Errorf("unexpected exit code %#+v while running %v", status.ExitCode(), fullCmd)
		}
	case <-e.ctx.Done():
		return e.ctx.Err()
	}
	return nil
}

func randID() string {
	h := crypto.SHA1.New()
	io.CopyN(h, rand.Reader, 10)
	return fmt.Sprintf("%x", h.Sum(nil))[:20]
}
