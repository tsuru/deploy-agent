// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/deploy-agent/internal/user"
	"github.com/tsuru/tsuru/app/bind"
	"github.com/tsuru/tsuru/exec"
)

var (
	defaultWorkingDir = "/home/application/current"
	tsuruYamlFiles    = []string{"tsuru.yml", "tsuru.yaml", "app.yml", "app.yaml"}
	appEnvsFile       = "/tmp/app_envs"
)

func execScript(cmds []string, envs []bind.EnvVar, w io.Writer, fs Filesystem, executor exec.Executor) error {
	if w == nil {
		w = ioutil.Discard
	}
	currentExecutor, err := user.ChangeUser(executor, envs)
	if err != nil {
		return err
	}
	workingDir := defaultWorkingDir
	exists, err := fs.CheckFile(defaultWorkingDir)
	if err != nil {
		return err
	}
	if exists == false {
		workingDir = "/"
	}
	formatedEnvs := []string{}
	for _, env := range envs {
		formatedEnv := fmt.Sprintf("%s=%s", env.Name, env.Value)
		formatedEnvs = append(formatedEnvs, formatedEnv)
	}
	if _, ok := executor.(*docker.Sidecar); !ok {
		// local environment variables do not make sense on a docker executor
		// since it runs commands in a different container
		formatedEnvs = append(formatedEnvs, os.Environ()...)
	}
	for _, cmd := range cmds {
		execOpts := exec.ExecuteOptions{
			Cmd:    "/bin/sh",
			Args:   []string{"-lc", cmd},
			Dir:    workingDir,
			Envs:   formatedEnvs,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
		fmt.Fprintf(w, " ---> Running %q\n", cmd)
		err := currentExecutor.Execute(execOpts)
		if err != nil {
			return fmt.Errorf("error running %q: %s\n", cmd, err)
		}
	}
	return nil
}

func loadTsuruYaml(fs Filesystem) (tsuru.TsuruYaml, error) {
	var tsuruYamlData tsuru.TsuruYaml
	for _, yamlFile := range tsuruYamlFiles {
		filePath := fmt.Sprintf("%s/%s", defaultWorkingDir, yamlFile)
		tsuruYaml, err := fs.ReadFile(filePath)
		if err != nil {
			continue
		}
		err = yaml.Unmarshal(tsuruYaml, &tsuruYamlData)
		if err != nil {
			return tsuru.TsuruYaml{}, err
		}
		break
	}
	return tsuruYamlData, nil
}

func buildHooks(yamlData tsuru.TsuruYaml, envs []bind.EnvVar, fs Filesystem, executor exec.Executor) error {
	cmds := append([]string{}, yamlData.Hooks.BuildHooks...)
	fmt.Fprintln(os.Stdout, "---- Running build hooks ----")
	return execScript(cmds, envs, os.Stdout, fs, executor)
}

func readProcfile(path string, fs Filesystem) (string, error) {
	procfile, err := fs.ReadFile(fmt.Sprintf("%v/Procfile", path))
	if err != nil {
		return "", err
	}
	return string(bytes.Replace(procfile, []byte("\r\n"), []byte("\n"), -1)), nil
}

var procfileRegex = regexp.MustCompile(`^([\w-]+):\s*(\S.+)$`)

func loadProcesses(t *tsuru.TsuruYaml, fs Filesystem) error {
	procfile, err := readProcfile(defaultWorkingDir, fs)
	if err != nil {
		return err
	}
	processList := strings.Split(procfile, "\n")
	processes := make(map[string]string, len(processList))
	for _, proc := range processList {
		if p := procfileRegex.FindStringSubmatch(proc); p != nil {
			processes[p[1]] = strings.Trim(p[2], " ")
		}
	}
	if len(processes) == 0 {
		return fmt.Errorf("invalid Procfile, no processes found in %q", procfile)
	}
	t.Processes = processes
	return nil
}

func readDiffDeploy(fs Filesystem) (string, bool, error) {
	filePath := fmt.Sprintf("%s/%s", defaultWorkingDir, "diff")
	deployDiff, err := fs.ReadFile(filePath)
	if err != nil {
		return "", true, err
	}
	defer fs.RemoveFile(filePath)
	return string(deployDiff), false, nil
}
