package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/docker/testing"
	"github.com/tsuru/deploy-agent/internal/sidecar"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
	"gopkg.in/check.v1"
)

func checkSkip(c *check.C) {
	if os.Getenv("DEPLOYAGENT_INTEGRATION") == "" {
		c.Skip("skipping integration tests")
	}
}

func (s *S) TestInspect(c *check.C) {
	checkSkip(c)

	_, cleanup, err := testing.SetupPrimaryContainer(c)
	c.Assert(err, check.IsNil)
	defer cleanup()

	sc, err := docker.NewSidecar(docker.SidecarConfig{User: "root"})
	c.Assert(err, check.IsNil)

	outW := new(bytes.Buffer)
	errW := new(bytes.Buffer)

	yamlData := `
hooks:
  build:
    - ps
`

	executor := sc.Executor(context.Background())

	err = executor.Execute(exec.ExecuteOptions{
		Cmd:    "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf("mkdir -p /home/application/current/ && echo '%s' > /home/application/current/tsuru.yaml", yamlData)},
		Stdout: outW,
		Stderr: errW,
	})
	c.Assert(err, check.IsNil)
	c.Assert(outW.String(), check.DeepEquals, "")
	c.Assert(errW.String(), check.DeepEquals, "")

	asUserExec, ok := executor.(interface {
		ExecuteAsUser(string, exec.ExecuteOptions) error
	})
	c.Assert(ok, check.Equals, true)

	for _, loc := range []string{"/", "/app/user/", "/home/application/current/"} {
		outW.Reset()
		errW.Reset()

		err = asUserExec.ExecuteAsUser("root", exec.ExecuteOptions{
			Cmd:    "/bin/sh",
			Args:   []string{"-c", fmt.Sprintf(`mkdir -p %s && echo '%s' > %sProcfile`, loc, loc, loc)},
			Stdout: outW,
			Stderr: errW,
		})
		c.Assert(err, check.IsNil)
		c.Assert(outW.String(), check.DeepEquals, "")
		c.Assert(errW.String(), check.DeepEquals, "")

		outW.Reset()
		errW.Reset()

		err = inspect(context.Background(), sc, "tsuru/base-platform", &executorFS{executor: executor}, outW, errW)
		c.Assert(err, check.IsNil)

		m := struct {
			Procfile  string
			TsuruYaml tsuru.TsuruYaml
			Image     sidecar.ImageInspect
		}{}
		err = json.NewDecoder(outW).Decode(&m)
		c.Assert(err, check.IsNil)
		c.Assert(outW.String(), check.DeepEquals, "")
		c.Assert(m.Procfile, check.DeepEquals, loc+"\n")
		c.Assert(m.TsuruYaml, check.DeepEquals, tsuru.TsuruYaml{Hooks: tsuru.Hook{BuildHooks: []string{"ps"}}})
		c.Assert(m.Image.ID, check.Not(check.DeepEquals), "")
	}

}
