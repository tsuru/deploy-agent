package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/tsuru/tsuru/exec"
	"gopkg.in/check.v1"
)

const primaryImage = "tsuru/base-platform"

func checkSkip(c *check.C) {
	if os.Getenv("DEPLOYAGENT_INTEGRATION") == "" {
		c.Skip("skipping integration tests")
	}
}

func (s *S) TestSidecarUploadToPrimaryContainerIntegration(c *check.C) {
	checkSkip(c)

	dClient, err := NewClient("")
	c.Assert(err, check.IsNil)

	pContID, err := setupPrimaryContainer(c, dClient)
	defer func(id string) {
		if id != "" {
			dClient.api.RemoveContainer(docker.RemoveContainerOptions{ID: id, Force: true})
		}
	}(pContID)
	c.Assert(err, check.IsNil)

	sidecar, err := NewSidecar(dClient, "")
	c.Assert(err, check.IsNil)

	err = sidecar.UploadToPrimaryContainer(context.Background(), "testdata/file.txt")
	c.Assert(err, check.IsNil)

	outBuff := new(bytes.Buffer)
	errBuff := new(bytes.Buffer)
	err = sidecar.Execute(exec.ExecuteOptions{
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

	dClient, err := NewClient("")
	c.Assert(err, check.IsNil)

	pContID, err := setupPrimaryContainer(c, dClient)
	defer func(id string) {
		if id != "" {
			dClient.api.RemoveContainer(docker.RemoveContainerOptions{ID: id, Force: true})
		}
	}(pContID)
	c.Assert(err, check.IsNil)

	sidecar, err := NewSidecar(dClient, "")
	c.Assert(err, check.IsNil)

	tt := []struct {
		Name string
		Cmd  string
		Args []string
		Envs []string
		Dir  string

		expectedOut    string
		expectedErrOut string
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
			Name:        "env-non-bash",
			Cmd:         "echo",
			Args:        []string{"$MYENV"},
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
	}

	for _, t := range tt {
		outBuff := new(bytes.Buffer)
		errBuff := new(bytes.Buffer)
		err = sidecar.Execute(exec.ExecuteOptions{
			Cmd:    t.Cmd,
			Args:   t.Args,
			Envs:   t.Envs,
			Dir:    t.Dir,
			Stdout: outBuff,
			Stderr: errBuff,
		})
		out, errOutput := outBuff.String(), errBuff.String()
		c.Check(err, check.IsNil, check.Commentf("[%v] error checking file uploaded: %v. Output: %v. Err output: %v", t.Name, err, out, errOutput))
		c.Check(out, check.DeepEquals, t.expectedOut, check.Commentf("[%v] unexpected output. Err output: %v", t.Name, errOutput))
		c.Check(errOutput, check.DeepEquals, t.expectedErrOut, check.Commentf("[%v] unexpected error output", t.Name))
	}

}

func setupPrimaryContainer(c *check.C, dClient *Client) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("error getting hostname: %v", err)
	}

	err = dClient.api.PullImage(docker.PullImageOptions{Repository: primaryImage}, docker.AuthConfiguration{})
	if err != nil {
		return "", fmt.Errorf("error pulling image %v: %v", primaryImage, err)
	}

	pCont, err := dClient.api.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image: primaryImage,
			Cmd:   []string{"/bin/sh", "-lc", "while true; do sleep 10; done"},
			Labels: map[string]string{
				"io.kubernetes.container.name": hostname,
				"io.kubernetes.pod.name":       hostname,
			},
		},
	})

	if err != nil {
		return "", fmt.Errorf("error creating primary container: %v", err)
	}

	err = dClient.api.StartContainer(pCont.ID, nil)
	if err != nil {
		return pCont.ID, fmt.Errorf("error starting primary container: %v", err)
	}

	timeout := time.After(time.Second * 15)
	for {
		c, err := dClient.api.InspectContainer(pCont.ID)
		if err != nil {
			return pCont.ID, fmt.Errorf("error inspecting primary container: %v", err)
		}
		if c.State.Running {
			break
		}
		select {
		case <-time.After(time.Second):
		case <-timeout:
			return pCont.ID, fmt.Errorf("timeout waiting for primary container to run")
		}
	}

	return pCont.ID, nil
}
