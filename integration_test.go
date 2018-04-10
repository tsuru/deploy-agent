package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/docker/testing"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
	"gopkg.in/check.v1"
)

const primaryImage = "tsuru/base-platform"

func checkSkip(c *check.C) {
	if os.Getenv("DEPLOYAGENT_INTEGRATION") == "" {
		c.Skip("skipping integration tests")
	}
}

func (s *S) TestInspect(c *check.C) {
	checkSkip(c)

	dClient, err := docker.NewClient("")
	c.Assert(err, check.IsNil)

	_, cleanup, err := testing.SetupPrimaryContainer(c)
	defer cleanup()

	sidecar, err := docker.NewSidecar(dClient, "root")
	c.Assert(err, check.IsNil)

	outW := new(bytes.Buffer)
	errW := new(bytes.Buffer)

	yamlData := `
hooks:
  build:
    - ps
`

	err = sidecar.Execute(exec.ExecuteOptions{
		Cmd:    "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf("mkdir -p /home/application/current/ && echo '%s' > /home/application/current/tsuru.yaml", yamlData)},
		Stdout: outW,
		Stderr: errW,
	})
	c.Assert(err, check.IsNil)
	c.Assert(outW.String(), check.DeepEquals, "")
	c.Assert(errW.String(), check.DeepEquals, "")

	for _, loc := range []string{"/", "/app/user/", "/home/application/current/"} {
		outW.Reset()
		errW.Reset()

		err = sidecar.ExecuteAsUser("root", exec.ExecuteOptions{
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

		err = inspect(dClient, "tsuru/base-platform", &executorFS{executor: sidecar}, outW, errW)
		c.Assert(err, check.IsNil)

		m := struct {
			Procfile  string
			TsuruYaml tsuru.TsuruYaml
			Image     docker.ImageInspect
		}{}
		err = json.NewDecoder(outW).Decode(&m)
		c.Assert(err, check.IsNil)
		c.Assert(outW.String(), check.DeepEquals, "")
		c.Assert(m.Procfile, check.DeepEquals, loc+"\n")
		c.Assert(m.TsuruYaml, check.DeepEquals, tsuru.TsuruYaml{Hooks: tsuru.Hook{BuildHooks: []string{"ps"}}})
		c.Assert(m.Image.ID, check.Not(check.DeepEquals), "")
	}

}
