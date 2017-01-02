// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/tsuru/tsuru/app/bind"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
	"gopkg.in/yaml.v2"
)

var (
	defaultWorkingDir = "/home/application/current"
	tsuruYamlFiles    = []string{"tsuru.yml", "tsuru.yaml", "app.yml", "app.yaml"}
	appEnvsFile       = "/tmp/app_envs"
)

var fsystem fs.Fs

func filesystem() fs.Fs {
	if fsystem == nil {
		fsystem = &fs.OsFs{}
	}
	return fsystem
}

var osExecutor exec.Executor

func executor() exec.Executor {
	if osExecutor == nil {
		return &exec.OsExecutor{}
	}
	return osExecutor
}
func execScript(cmds []string, envs []bind.EnvVar) error {
	workingDir := defaultWorkingDir
	if _, err := filesystem().Stat(defaultWorkingDir); err != nil {
		if os.IsNotExist(err) {
			workingDir = "/"
		} else {
			return err
		}
	}
	formatedEnvs := []string{}
	for _, env := range envs {
		formatedEnv := fmt.Sprintf("%s=%s", env.Name, env.Value)
		formatedEnvs = append(formatedEnvs, formatedEnv)
	}
	formatedEnvs = append(formatedEnvs, os.Environ()...)
	errors := make(chan error, len(cmds))
	for _, cmd := range cmds {
		execOpts := exec.ExecuteOptions{
			Cmd:    "/bin/bash",
			Args:   []string{"-lc", cmd},
			Dir:    workingDir,
			Envs:   formatedEnvs,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
		err := executor().Execute(execOpts)
		if err != nil {
			errors <- err
		}
	}
	close(errors)
	formatedErrors := ""
	for e := range errors {
		formatedErrors += fmt.Sprintf("%s\n", e)
	}
	if formatedErrors != "" {
		return fmt.Errorf("%s", formatedErrors)
	}
	return nil
}

type TsuruYaml struct {
	Hooks       Hook                   `json:"hooks,omitempty"`
	Processes   map[string]string      `json:"processes,omitempty"`
	Healthcheck map[string]interface{} `yaml:"healthcheck" json:"healthcheck,omitempty"`
}

type Hook struct {
	BuildHooks []string               `yaml:"build,omitempty" json:"build"`
	Restart    map[string]interface{} `yaml:"restart" json:"restart"`
}

func (t *TsuruYaml) isEmpty() bool {
	return len(t.Hooks.BuildHooks) == 0 && t.Processes == nil
}
func loadTsuruYaml() (TsuruYaml, error) {
	var tsuruYamlData TsuruYaml
	for _, yamlFile := range tsuruYamlFiles {
		filePath := fmt.Sprintf("%s/%s", defaultWorkingDir, yamlFile)
		f, err := filesystem().Open(filePath)
		if err != nil {
			continue
		}
		defer f.Close()
		tsuruYaml, err := ioutil.ReadAll(f)
		if err != nil {
			return TsuruYaml{}, err
		}
		err = yaml.Unmarshal(tsuruYaml, &tsuruYamlData)
		if err != nil {
			return TsuruYaml{}, err
		}
		break
	}
	return tsuruYamlData, nil
}

func buildHooks(yamlData TsuruYaml, envs []bind.EnvVar) error {
	cmds := append([]string{}, yamlData.Hooks.BuildHooks...)
	return execScript(cmds, envs)
}

func readProcfile() (string, error) {
	procfilePath := fmt.Sprintf("%s/%s", defaultWorkingDir, "Procfile")
	f, err := filesystem().Open(procfilePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	procfile, err := ioutil.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(procfile), nil
}

var procfileRegex = regexp.MustCompile(`^([\w-]+):\s*(\S.+)$`)

func loadProcesses(t *TsuruYaml) error {
	procfile, err := readProcfile()
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
	t.Processes = processes
	return nil
}

func saveAppEnvsFile(envs []bind.EnvVar) error {
	f, err := filesystem().Create(appEnvsFile)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, e := range envs {
		f.Write([]byte(fmt.Sprintf("export %s='%s'\n", e.Name, e.Value)))
	}
	return nil
}

func readDiffDeploy() (string, bool, error) {
	filePath := fmt.Sprintf("%s/%s", defaultWorkingDir, "diff")
	f, err := filesystem().Open(filePath)
	defer f.Close()
	defer filesystem().Remove(filePath)
	if err != nil {
		return "", true, nil
	}
	deployDiff, err := ioutil.ReadAll(f)
	if err != nil {
		return "", true, err
	}
	return string(deployDiff), false, nil
}
