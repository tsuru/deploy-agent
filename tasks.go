package main

import (
	"fmt"
	"github.com/tsuru/tsuru/app/bind"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"strings"
)

var (
	workingDir     = "/home/application/current"
	tsuruYamlFiles = []string{"tsuru.yml", "tsuru.yaml", "app.yml", "app.yaml"}
)

var osExecutor exec.Executor

func executor() exec.Executor {
	if osExecutor == nil {
		return &exec.OsExecutor{}
	}
	return osExecutor
}
func execScript(cmds []string, envs []bind.EnvVar) error {
	formatedEnvs := []string{}
	for _, env := range envs {
		formatedEnv := fmt.Sprintf("%s=%s", env.Name, env.Value)
		formatedEnvs = append(formatedEnvs, formatedEnv)
	}
	errors := make(chan error, len(cmds))
	for _, cmd := range cmds {
		execOpts := exec.ExecuteOptions{
			Cmd:  cmd,
			Dir:  workingDir,
			Envs: formatedEnvs,
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

var fsystem fs.Fs

func filesystem() fs.Fs {
	if fsystem == nil {
		fsystem = &fs.OsFs{}
	}
	return fsystem
}

type TsuruYaml struct {
	Hooks    BuildHook         `json:"hooks"`
	Process  map[string]string `json:"process"`
	Procfile string            `json:"procfile"`
}

type BuildHook struct {
	BuildHooks []string `yaml:"build,omitempty" json:"build"`
}

func loadTsuruYaml() (TsuruYaml, error) {
	var tsuruYamlData TsuruYaml
	for _, yamlFile := range tsuruYamlFiles {
		filePath := fmt.Sprintf("%s/%s", workingDir, yamlFile)
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
	var cmds []string
	for _, cmd := range yamlData.Hooks.BuildHooks {
		cmds = append(cmds, fmt.Sprintf("%s %s %s", "/bin/bash", "-lc", cmd))
	}
	return execScript(cmds, envs)
}

func readProcfile() (string, error) {
	procfilePath := fmt.Sprintf("%s/%s", workingDir, "Procfile")
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

func loadProcfile(t *TsuruYaml) error {
	procfile, err := readProcfile()
	if err != nil {
		return err
	}
	t.Procfile = procfile
	return nil
}

func loadProcess(t *TsuruYaml) error {
	procfile, err := readProcfile()
	if err != nil {
		return err
	}
	process := map[string]string{}
	processes := strings.Split(procfile, "\n")
	for _, proc := range processes {
		p := strings.SplitN(proc, ":", 2)
		process[p[0]] = strings.Trim(p[1], " ")
	}
	t.Process = process
	return nil
}
