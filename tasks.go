package main

import (
	"fmt"
	"os/exec"
	"sync"
)

var (
	workingDir string = "/home/application/current"
)

func execStartScript(cmds []string, envs map[string]interface{}) error {
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
			cmd.Dir = workingDir
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
