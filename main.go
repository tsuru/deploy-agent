// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
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

	c := tsuru.Client{
		URL:     os.Args[1],
		Token:   os.Args[2],
		Version: version,
	}
	appName := os.Args[3]
	command := os.Args[4:]

	var filesystem Filesystem = &localFS{Fs: fs.OsFs{}}
	var executor exec.Executor = &exec.OsExecutor{}

	sideCar := os.Getenv("DEPLOYAGENT_RUN_AS_SIDECAR") == "true"

	if sideCar {
		dockerClient, err := docker.NewClient(os.Getenv("DOCKER_HOST"))
		if err != nil {
			fatal("failed to create docker client: %v", err)

		}
		hostname, err := os.Hostname()
		if err != nil {
			fatal("failed to get hostname: %v", err)
		}
		timeout := time.After(time.Second * 30)
		var containers []docker.Container
	loop:
		for {
			containers, err = dockerClient.ListContainersByLabels(map[string]string{
				"io.kubernetes.container.name": hostname,
				"io.kubernetes.pod.name":       hostname,
			})
			if err != nil {
				fatal("failed to get containers: %v", err)
			}
			select {
			case <-timeout:
				break loop
			case <-time.After(time.Second * 3):
				break
			}
		}
		if len(containers) != 1 {
			fatal("failed to get main container from sidecar, got %v.", containers)
		}
		executor = &docker.Executor{Client: dockerClient, ContainerID: containers[0].ID}
		filesystem = &executorFS{executor: executor}
		defer func() {
			fmt.Println("---- Building application image ----")
			imgName := os.Getenv("DEPLOYAGENT_DST_IMAGE")
			img, err := dockerClient.Commit(context.Background(), containers[0].ID, imgName)
			if err != nil {
				fatal("error commiting image %v: %v", imgName, err)
			}
			err = dockerClient.Tag(context.Background(), img)
			if err != nil {
				fatal("error tagging image %v: %v", img, err)
			}
			fmt.Printf(" ---> Sending image to repository (%s)\n", img)
			err = dockerClient.Push(context.Background(), img)
			if err != nil {
				fatal("error pushing image %v: %v", img, err)
			}
		}()
	}

	var err error
	switch command[len(command)-1] {
	case "build":
		err = build(c, appName, command[:len(command)-1], filesystem, executor)
	case "deploy-only":
		err = deploy(c, appName, filesystem, executor)
	case "deploy":
		// backward compatibility with tsuru < 1.4.0
		command = command[:len(command)-1]
		fallthrough
	default:
		err = build(c, appName, command, filesystem, executor)
		if err != nil {
			break
		}
		err = deploy(c, appName, filesystem, executor)
	}
	if err != nil {
		fatal("[deploy-agent] error: %v", err)
	}
}

func fatal(format string, v ...interface{}) {
	var w io.Writer = os.Stderr
	file, err := os.OpenFile("/dev/termination-log", os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		fmt.Fprint(w, "failed to open termination-log file")
	} else {
		w = file
		defer func() {
			errClose := file.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to close termination file: %v", errClose)
			}
		}()
	}
	fmt.Fprintf(w, format, v...)
	os.Exit(1)
}
