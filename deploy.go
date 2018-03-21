// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"

	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
)

func build(c tsuru.Client, appName string, cmd []string, fs Filesystem, executor exec.Executor) error {
	envs, err := c.GetAppEnvs(appName)
	if err != nil {
		return err
	}
	return execScript(cmd, envs, os.Stdout, fs, executor)
}

func deploy(c tsuru.Client, appName string, fs Filesystem, executor exec.Executor) error {
	var yamlData tsuru.TsuruYaml
	envs, err := c.RegisterUnit(appName, yamlData)
	if err != nil {
		return err
	}
	diff, firstDeploy, err := readDiffDeploy(fs)
	if !firstDeploy && err == nil {
		err = c.SendDiffDeploy(diff, appName)
		if err != nil {
			return err
		}
	}
	yamlData, err = loadTsuruYaml(fs)
	if err != nil {
		return err
	}
	err = buildHooks(yamlData, envs, fs, executor)
	if err != nil {
		return err
	}
	err = loadProcesses(&yamlData, fs)
	if err != nil {
		return err
	}
	_, err = c.RegisterUnit(appName, yamlData)
	return err
}
