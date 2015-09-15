// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func deployAgent(args []string) {
	c := Client{
		URL:   args[0],
		Token: args[1],
	}
	envs, err := c.registerUnit(args[2], nil)
	if err != nil {
		panic(err)
	}
	err = execScript(args[3:], envs)
	if err != nil {
		panic(err)
	}
	yamlData, err := loadTsuruYaml()
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
}
