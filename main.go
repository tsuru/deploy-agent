// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tsuru/tsuru/exec"
)

const version = "0.2.8"

func main() {
	var (
		printVersion bool
	)
	flag.BoolVar(&printVersion, "version", false, "Print version and exit")
	flag.Parse()

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

	switch command[len(command)-1] {
	case "build":
		build(c, appName, command[:len(command)-1], &localFS{}, &exec.OsExecutor{})
	case "deploy-only":
		deploy(c, appName, &localFS{}, &exec.OsExecutor{})
	case "deploy":
		// backward compatibility with tsuru < 1.4.0
		command = command[:len(command)-1]
		fallthrough
	default:
		build(c, appName, command, &localFS{}, &exec.OsExecutor{})
		deploy(c, appName, &localFS{}, &exec.OsExecutor{})
	}
}
