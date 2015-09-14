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

func loadTsuruYaml() (map[string]interface{}, error) {
	var tsuruYamlData map[string]interface{}
	for _, yamlFile := range tsuruYamlFiles {
		filePath := fmt.Sprintf("%s/%s", workingDir, yamlFile)
		f, err := filesystem().Open(filePath)
		if err != nil {
			continue
		}
		defer f.Close()
		tsuruYaml, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, err
		}
		err = yaml.Unmarshal(tsuruYaml, &tsuruYamlData)
		if err != nil {
			return nil, err
		}
		break
	}
	return tsuruYamlData, nil
}
