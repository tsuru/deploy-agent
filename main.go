// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.2.0"

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
	deployAgent(os.Args[1:])
}
