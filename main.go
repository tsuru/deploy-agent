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

	"github.com/kelseyhightower/envconfig"
	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
)

const version = "0.2.8"

type Config struct {
	DockerHost          string `envconfig:"DOCKER_HOST"`
	RunAsSidecar        bool
	DestinationImage    string
	RegistryPushRetries int `default:"3"`
	RegistryAuthEmail   string
	RegistryAuthPass    string
	RegistryAuthUser    string
	RegistryAddress     string
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
		fatal("error processing environment variables: %v", err)
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

	if config.RunAsSidecar {
		dockerClient, err := docker.NewClient(config.DockerHost)
		if err != nil {
			fatal("failed to create docker client: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		mainContainer, err := getMainContainer(ctx, dockerClient)
		cancel()
		if err != nil {
			fatal("failed to get main container: %v", err)
		}
		executor = &docker.Executor{Client: dockerClient, ContainerID: mainContainer.ID}
		filesystem = &executorFS{executor: executor}
		defer func() {
			fmt.Println("---- Building application image ----")
			img, err := dockerClient.Commit(context.Background(), mainContainer.ID, config.DestinationImage)
			if err != nil {
				fatal("error commiting image %v: %v", config.DestinationImage, err)
			}
			err = dockerClient.Tag(context.Background(), img)
			if err != nil {
				fatal("error tagging image %v: %v", img, err)
			}
			fmt.Printf(" ---> Sending image to repository (%s)\n", img)
			authConfig := docker.AuthConfig{
				Username:      config.RegistryAuthUser,
				Password:      config.RegistryAuthPass,
				Email:         config.RegistryAuthEmail,
				ServerAddress: config.RegistryAddress,
			}
			for i := 0; i < config.RegistryPushRetries; i++ {
				err = dockerClient.Push(context.Background(), authConfig, img)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Could not send image, trying again. Original error: %v\n", err)
					time.Sleep(time.Second)
					continue
				}
				break
			}
			if err != nil {
				fatal("Error pushing image: %v", err)
			}
		}()
	}

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

func getMainContainer(ctx context.Context, dockerClient *docker.Client) (docker.Container, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return docker.Container{}, fmt.Errorf("failed to get hostname: %v", err)
	}
	for {
		containers, err := dockerClient.ListContainersByLabels(ctx, map[string]string{
			"io.kubernetes.container.name": hostname,
			"io.kubernetes.pod.name":       hostname,
		})
		if err != nil {
			return docker.Container{}, fmt.Errorf("failed to get containers: %v", err)
		}
		if len(containers) == 1 {
			return containers[0], nil
		}
		select {
		case <-ctx.Done():
			return docker.Container{}, ctx.Err()
		case <-time.After(time.Second * 1):
		}
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
