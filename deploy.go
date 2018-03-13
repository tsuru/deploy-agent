// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"

	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
)

func build(c tsuru.Client, appName string, cmd []string, fs Filesystem, executor exec.Executor) {
	log.SetFlags(0)
	envs, err := c.GetAppEnvs(appName)
	if err != nil {
		log.Fatal(err)
	}
	err = execScript(cmd, envs, nil, fs, executor)
	if err != nil {
		log.Fatal(err)
	}
}

func deploy(c tsuru.Client, appName string, fs Filesystem, executor exec.Executor) {
	log.SetFlags(0)
	var yamlData tsuru.TsuruYaml
	envs, err := c.RegisterUnit(appName, yamlData)
	if err != nil {
		log.Fatal(err)
	}
	diff, firstDeploy, err := readDiffDeploy(fs)
	if err != nil {
		log.Fatal(err)
	}
	if !firstDeploy {
		err = c.SendDiffDeploy(diff, appName)
		if err != nil {
			log.Fatal(err)
		}
	}
	yamlData, err = loadTsuruYaml(fs)
	if err != nil {
		log.Fatal(err)
	}
	err = buildHooks(yamlData, envs, fs, executor)
	if err != nil {
		log.Fatal(err)
	}
	err = loadProcesses(&yamlData, fs)
	if err != nil {
		log.Fatal(err)
	}
	_, err = c.RegisterUnit(appName, yamlData)
	if err != nil {
		log.Fatal(err)
	}
}
