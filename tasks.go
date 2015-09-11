package main

import (
	"fmt"
	"github.com/tsuru/tsuru/fs"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os/exec"
	"sync"
)

var (
	workingDir     = "/home/application/current"
	tsuruYamlFiles = []string{"tsuru.yml", "tsuru.yaml", "app.yml", "app.yaml"}
)

func execScript(cmds []string, envs map[string]interface{}) error {
	formatedEnvs := []string{}
	for k, env := range envs {
		formatedEnv := fmt.Sprintf("%s=%s", k, env)
		formatedEnvs = append(formatedEnvs, formatedEnv)
	}
	errors := make(chan error, len(cmds))
	var wg sync.WaitGroup
	for _, cmd := range cmds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command("/bin/bash", "-lc", cmd)
			cmd.Env = formatedEnvs
			err := cmd.Run()
			if err != nil {
				errors <- err
			}
		}()
	}
	wg.Wait()
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
