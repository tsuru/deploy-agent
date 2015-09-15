package main

import (
	"fmt"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
	"gopkg.in/yaml.v2"
	"io/ioutil"
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
func execScript(cmds []string, envs map[string]interface{}) error {
	formatedEnvs := []string{}
	for k, env := range envs {
		formatedEnv := fmt.Sprintf("%s=%s", k, env)
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
	Hooks    BuildHook `yaml:hooks`
	Proccess map[string]string
	Procfile string
}

type BuildHook struct {
	BuildHooks []string `yaml:"build,omitempty"`
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

func buildHooks(yamlData TsuruYaml, envs map[string]interface{}) error {
	var cmds []string
	for _, cmd := range yamlData.Hooks.BuildHooks {
		cmds = append(cmds, fmt.Sprintf("%s %s %s", "/bin/bash", "-lc", cmd))
	}
	return execScript(cmds, envs)
}

func loadProcfile(t *TsuruYaml) error {
	procfilePath := fmt.Sprintf("%s/%s", workingDir, "Procfile")
	f, err := filesystem().Open(procfilePath)
	if err != nil {
		return err
	}
	defer f.Close()
	procfile, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	t.Procfile = string(procfile)
	return nil
}