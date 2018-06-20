// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
)

const version = "0.2.8"

type Config struct {
	DockerHost          string   `envconfig:"DOCKER_HOST"`
	RunAsSidecar        bool     `split_words:"true"`
	DestinationImages   []string `split_words:"true"`
	SourceImage         string   `split_words:"true"`
	InputFile           string   `split_words:"true"`
	DockerfileBuild     bool     `split_words:"true"`
	RegistryPushRetries int      `split_words:"true" default:"3"`
	RegistryAuthEmail   string   `split_words:"true"`
	RegistryAuthPass    string   `split_words:"true"`
	RegistryAuthUser    string   `split_words:"true"`
	RegistryAddress     string   `split_words:"true"`
	RunAsUser           string   `split_words:"true"`
}

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

	var config Config
	err := envconfig.Process("deployagent", &config)
	if err != nil {
		fatalf("error processing environment variables: %v", err)
	}

	var filesystem Filesystem = &localFS{Fs: fs.OsFs{}}
	var executor exec.Executor = &exec.OsExecutor{}

	if config.RunAsSidecar {
		dockerClient, err := docker.NewClient(config.DockerHost)
		if err != nil {
			fatalf("failed to create docker client: %v", err)
		}
		if config.DockerfileBuild {
			if err := buildAndPush(dockerClient, config.DestinationImages[0], config.InputFile, config, os.Stdout); err != nil {
				fatalf("failed to build and push image: %v", err)
			}
			return
		}
		sideCar, err := setupSidecar(dockerClient, config)
		if err != nil {
			fatalf("failed to create sidecar: %v", err)
		}
		executor = sideCar
		filesystem = &executorFS{executor: sideCar}
		if config.SourceImage != "" {
			// build/deploy/deploy-only is not required since this is an image deploy
			// all we need to do is return the inspected files and image and push the
			// destination images based on the sidecar container.
			if err := inspect(dockerClient, config.SourceImage, filesystem, os.Stdout, os.Stderr); err != nil {
				fatalf("error inspecting sidecar: %v", err)
			}

			if err := tagAndPushDestinations(dockerClient, config.SourceImage, config, os.Stdout); err != nil {
				fatalf("error pushing images: %v", err)
			}
			return
		}

		// we defer the call to pushSidecar so the normal build/deploy steps are executed
		// by the sidecar executor. This will only be executed if those steps finish without
		// any error since the call to fatal() exits.
		defer pushSidecar(dockerClient, sideCar, config, os.Stdout)
	}

	c := tsuru.Client{
		URL:     os.Args[1],
		Token:   os.Args[2],
		Version: version,
	}
	appName := os.Args[3]
	command := os.Args[4:]

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
		fatalf("[deploy-agent] error: %v", err)
	}
}

func fatalf(format string, v ...interface{}) {
	file, err := os.OpenFile("/dev/termination-log", os.O_WRONLY|os.O_CREATE, 0666)
	if err == nil {
		fmt.Fprintf(file, format, v...)
		errClose := file.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to close termination file: %v", errClose)
		}
	}
	fmt.Fprintf(os.Stderr, format, v...)
	os.Exit(1)
}
