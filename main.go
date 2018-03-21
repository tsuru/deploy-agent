// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
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
	RunAsSidecar        bool   `split_words:"true"`
	DestinationImage    string `split_words:"true"`
	InputFile           string `split_words:"true"`
	RegistryPushRetries int    `split_words:"true" default:"3"`
	RegistryAuthEmail   string `split_words:"true"`
	RegistryAuthPass    string `split_words:"true"`
	RegistryAuthUser    string `split_words:"true"`
	RegistryAddress     string `split_words:"true"`
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
		err = uploadFile(context.Background(), dockerClient, mainContainer.ID, config.InputFile)
		if err != nil {
			fatal("failed to upload input file: %v", err)
		}
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

func uploadFile(ctx context.Context, dockerClient *docker.Client, container, inputFile string) error {
	file, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file %q: %v", inputFile, err)
	}
	defer func() {
		if file.Close() != nil {
			fmt.Fprintf(os.Stderr, "error closing file %q: %v", inputFile, err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat input file: %v", err)
	}
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: file.Name(),
		Mode: 0777,
		Size: info.Size(),
	}); err != nil {
		return fmt.Errorf("failed to write archive header: %v", err)
	}
	n, err := io.Copy(tw, file)
	if err != nil {
		return fmt.Errorf("failed to write file to archive: %v", err)
	}
	if n != info.Size() {
		return errors.New("short-write copying to archive")
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close archive: %v", err)
	}
	return dockerClient.Upload(ctx, container, "/", buf)
}

func fatal(format string, v ...interface{}) {
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
