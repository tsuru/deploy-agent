// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.2.4"

var printVersion bool

func init() {
	flag.BoolVar(&printVersion, "version", false, "Print version and exit")
	flag.Parse()
}

func main() {
	if printVersion {
		fmt.Printf("deploy-agent version %s\n", version)
		return
	}
	c := Client{
		URL:   os.Args[1],
		Token: os.Args[2],
	}
	appName := os.Args[3]
	command := os.Args[4:]
	if command[len(command)-1] == "build" {
		build(c, appName, command[:len(command)-1])
		return
	}
	if command[len(command)-1] == "deploy-only" {
		deploy(c, appName)
		return
	}
	// backward compatibility with tsuru < 1.4.0
	if command[len(command)-1] == "deploy" {
		command = command[:len(command)-1]
	}
	build(c, appName, command)
	deploy(c, appName)
}
