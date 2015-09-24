// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func deployAgent(args []string) {
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
		panic(err)
	}
	err = execScript(args[3:], envs)
	if err != nil {
		panic(err)
	}
	yamlData, err = loadTsuruYaml()
	if err != nil {
		panic(err)
	}
	err = buildHooks(yamlData, envs)
	if err != nil {
		panic(err)
	}
	err = loadProcfile(&yamlData)
	if err != nil {
		panic(err)
	}
	err = loadProcess(&yamlData)
	if err != nil {
		panic(err)
	}
	_, err = c.registerUnit(args[2], yamlData)
	if err != nil {
		panic(err)
	}
}
