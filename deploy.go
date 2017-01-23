// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"
)

func deployAgent(args []string) {
	log.SetFlags(0)
	// backward compatibility with tsuru 0.12.x
	if args[len(args)-1] == "deploy" {
		args = args[:len(args)-1]
	}
	c := Client{
		URL:   args[0],
		Token: args[1],
	}
	var yamlData TsuruYaml
	envs, err := c.registerUnit(args[2], yamlData)
	if err != nil {
		log.Fatal(err)
	}
	err = saveAppEnvsFile(envs)
	if err != nil {
		log.Fatal(err)
	}
	err = execScript(args[3:], envs)
	if err != nil {
		log.Fatal(err)
	}
	diff, firstDeploy, err := readDiffDeploy()
	if err != nil {
		log.Fatal(err)
	}
	if !firstDeploy {
		err = c.sendDiffDeploy(diff, args[2])
		if err != nil {
			log.Fatal(err)
		}
	}
	yamlData, err = loadTsuruYaml()
	if err != nil {
		log.Fatal(err)
	}
	err = buildHooks(yamlData, envs)
	if err != nil {
		log.Fatal(err)
	}
	err = loadProcesses(&yamlData)
	if err != nil {
		log.Fatal(err)
	}
	_, err = c.registerUnit(args[2], yamlData)
	if err != nil {
		log.Fatal(err)
	}
}
