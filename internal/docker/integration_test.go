package docker

import (
	"bytes"
	"context"
	"os"

	"github.com/tsuru/deploy-agent/internal/docker/testing"
	"github.com/tsuru/tsuru/exec"
	"gopkg.in/check.v1"
)

func checkSkip(c *check.C) {
	if os.Getenv("DEPLOYAGENT_INTEGRATION") == "" {
		c.Skip("skipping integration tests")
	}
}

func (s *S) TestSidecarUploadToPrimaryContainerIntegration(c *check.C) {
	checkSkip(c)

	_, cleanup, err := testing.SetupPrimaryContainer(c)
	defer cleanup()
	c.Assert(err, check.IsNil)

	sidecar, err := NewSidecar("", "")
	c.Assert(err, check.IsNil)

	err = sidecar.Upload(context.Background(), "testdata/file.txt")
	c.Assert(err, check.IsNil)

	outBuff := new(bytes.Buffer)
	errBuff := new(bytes.Buffer)
	err = sidecar.Executor(context.Background()).Execute(exec.ExecuteOptions{
		Cmd:    "/bin/sh",
		Args:   []string{"-lc", "cat /testdata/file.txt"},
		Stdout: outBuff,
		Stderr: errBuff,
	})
	out, errOutput := outBuff.String(), errBuff.String()
	c.Assert(err, check.IsNil, check.Commentf("error checking file uploaded: %v. Output: %v. Error: %v", err, out, errOutput))
	c.Assert(out, check.DeepEquals, "file data", check.Commentf("unexpected filed content: %v. Err output: %v", out, errOutput))
	c.Assert(errOutput, check.DeepEquals, "")
}

func (s *S) TestSidecarExecuteIntegration(c *check.C) {
	checkSkip(c)

	_, cleanup, err := testing.SetupPrimaryContainer(c)
	defer cleanup()
	c.Assert(err, check.IsNil)

	sidecar, err := NewSidecar("", "")
	c.Assert(err, check.IsNil)

	err = sidecar.Executor(context.Background()).Execute(exec.ExecuteOptions{
		Dir:  "/",
		Cmd:  "/bin/sh",
		Args: []string{"-c", `echo '#!/bin/bash -el' >/tmp/myscript; echo 'echo hey; exit 0; echo done' >>/tmp/myscript; chmod +x /tmp/myscript`},
	})
	c.Assert(err, check.IsNil)

	tt := []struct {
		Name string
		Cmd  string
		Args []string
		Envs []string
		Dir  string

		expectedOut    string
		expectedErrOut string
		expectedErr    string
	}{
		{
			Name:        "simple",
			Cmd:         "/bin/sh",
			Args:        []string{"-lc", "echo simple"},
			expectedOut: "simple\n",
		},
		{
			Name:        "simple-non-bash",
			Cmd:         "echo",
			Args:        []string{"simple"},
			expectedOut: "simple\n",
		},
		{
			Name:        "change-dir",
			Cmd:         "/bin/sh",
			Args:        []string{"-lc", "pwd"},
			Dir:         "/home/application",
			expectedOut: "/home/application\n",
		},
		{
			Name:        "change-dir-non-bash",
			Cmd:         "pwd",
			Dir:         "/home/application",
			expectedOut: "/home/application\n",
		},
		{
			Name:        "env",
			Cmd:         "/bin/sh",
			Args:        []string{"-lc", "echo $MYENV"},
			Envs:        []string{"MYENV=myval", "ANOTHERENV=anotherval"},
			expectedOut: "myval\n",
		},
		{
			Name:        "dir-env",
			Cmd:         "/bin/sh",
			Args:        []string{"-lc", "pwd"},
			Envs:        []string{"MYDIR=/etc"},
			Dir:         "$MYDIR",
			expectedOut: "/etc\n",
		},

		{
			Name:        "dir-env-non-bash",
			Cmd:         "pwd",
			Envs:        []string{"MYDIR=/etc"},
			Dir:         "$MYDIR",
			expectedOut: "/etc\n",
		},
		{
			Name:        "env-with-quotes",
			Cmd:         "/bin/sh",
			Args:        []string{"-lc", "echo $MYENV"},
			Envs:        []string{"MYENV={\"a\": \"b\", \"a2\": \"b2\"}"},
			expectedOut: "{\"a\": \"b\", \"a2\": \"b2\"}\n",
		},
		{
			Name:        "env-with-quotes",
			Cmd:         "/bin/sh",
			Args:        []string{"-lc", "echo $MYENV"},
			Envs:        []string{"MYENV={'a': 'b', 'a2': 'b2'}"},
			expectedOut: "{'a': 'b', 'a2': 'b2'}\n",
		},
		{
			Name:        "exit-with-error",
			Cmd:         "/bin/sh",
			Dir:         "/",
			Args:        []string{"-lc", "exit 99"},
			expectedErr: `unexpected exit code 99 while running.*`,
		},
		{
			Name:        "env-and-exit",
			Cmd:         "/bin/sh",
			Dir:         "/",
			Args:        []string{"-lc", "/tmp/myscript xxx"},
			Envs:        []string{"MYENV={}", "OTHERENV=x"},
			expectedOut: "hey\n",
		},
	}

	for _, t := range tt {
		outBuff := new(bytes.Buffer)
		errBuff := new(bytes.Buffer)
		err = sidecar.Executor(context.Background()).Execute(exec.ExecuteOptions{
			Cmd:    t.Cmd,
			Args:   t.Args,
			Envs:   t.Envs,
			Dir:    t.Dir,
			Stdout: outBuff,
			Stderr: errBuff,
		})
		out, errOutput := outBuff.String(), errBuff.String()
		if t.expectedErr != "" {
			c.Check(err, check.ErrorMatches, t.expectedErr)
		} else {
			c.Check(err, check.IsNil, check.Commentf("[%v] error checking file uploaded: %v. Output: %v. Err output: %v", t.Name, err, out, errOutput))
		}
		c.Check(out, check.DeepEquals, t.expectedOut, check.Commentf("[%v] unexpected output. Err output: %v", t.Name, errOutput))
		c.Check(errOutput, check.DeepEquals, t.expectedErrOut, check.Commentf("[%v] unexpected error output", t.Name))
	}
}

func (s *S) TestSidecarExecuteAsUserIntegration(c *check.C) {
	checkSkip(c)

	_, cleanup, err := testing.SetupPrimaryContainer(c)
	defer cleanup()
	c.Assert(err, check.IsNil)

	sidecar, err := NewSidecar("", "")
	c.Assert(err, check.IsNil)

	tt := []struct {
		user           string
		expectedOutput string
	}{
		{user: "ubuntu", expectedOutput: "ubuntu\n"},
		{user: "", expectedOutput: "ubuntu\n"},
		{user: "root", expectedOutput: "root\n"},
		{user: "1000", expectedOutput: "ubuntu\n"},
	}

	for _, t := range tt {
		outBuff := new(bytes.Buffer)
		errBuff := new(bytes.Buffer)

		executor := sidecar.Executor(context.Background())
		asUserExec, ok := executor.(interface {
			ExecuteAsUser(string, exec.ExecuteOptions) error
		})
		c.Assert(ok, check.Equals, true)

		err := asUserExec.ExecuteAsUser(t.user, exec.ExecuteOptions{
			Cmd:    "whoami",
			Stdout: outBuff,
			Stderr: errBuff,
		})

		out, errOutput := outBuff.String(), errBuff.String()
		c.Check(err, check.IsNil, check.Commentf("[%v] error running as user: %v", t.user, err))
		c.Check(out, check.DeepEquals, t.expectedOutput, check.Commentf("[%v] unexpected output. Err output: %v", t.user, errOutput))
	}
}
