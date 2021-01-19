// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/tsuru/deploy-agent/internal/containerd"
	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/sidecar"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
)

const version = "0.8.4"

type Config struct {
	DockerHost          string   `envconfig:"DOCKER_HOST"`
	ContainerdAddress   string   `envconfig:"CONTAINERD_ADDRESS" default:"/run/containerd/containerd.sock"`
	DestinationImages   []string `split_words:"true"`
	SourceImage         string   `split_words:"true"`
	InputFile           string   `split_words:"true"`
	RegistryAuthEmail   string   `split_words:"true"`
	RegistryAuthPass    string   `split_words:"true"`
	RegistryAuthUser    string   `split_words:"true"`
	RegistryAddress     string   `split_words:"true"`
	RunAsUser           string   `split_words:"true"`
	BuildctlCmd         string   `split_words:"true" default:"buildctl-daemonless.sh"`
	RegistryPushRetries int      `split_words:"true" default:"3"`
	RunAsSidecar        bool     `split_words:"true"`
	DockerfileBuild     bool     `split_words:"true"`
	InsecureRegistry    bool     `split_words:"true"`
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

	err := runAgent()
	if err != nil {
		fatalf("[deploy-agent] error: %v", err)
	}
}

func runAgent() error {
	var config Config
	err := envconfig.Process("deployagent", &config)
	if err != nil {
		return fmt.Errorf("error processing environment variables: %v", err)
	}

	var filesystem Filesystem = &localFS{Fs: fs.OsFs{}}
	var executor exec.Executor = &exec.OsExecutor{}

	ctx := context.TODO()
	regConfig := sidecar.RegistryConfig{
		RegistryAuthUser:    config.RegistryAuthUser,
		RegistryAuthPass:    config.RegistryAuthPass,
		RegistryAddress:     config.RegistryAddress,
		RegistryPushRetries: config.RegistryPushRetries,
	}

	var sc sidecar.Sidecar

	if config.RunAsSidecar {
		sc, err = docker.NewSidecar(docker.SidecarConfig{
			Address:    config.DockerHost,
			User:       config.RunAsUser,
			Standalone: config.DockerfileBuild,
		})
		if err != nil {
			var containerdErr error
			sc, containerdErr = containerd.NewSidecar(ctx, containerd.SidecarConfig{
				Address:          config.ContainerdAddress,
				User:             config.RunAsUser,
				BuildctlCmd:      config.BuildctlCmd,
				InsecureRegistry: config.InsecureRegistry,
				Standalone:       config.DockerfileBuild,
			})
			if containerdErr != nil {
				return fmt.Errorf("failed to initialize both docker and containerd: docker error: %v, containerd error: %v", err, containerdErr)
			}
		}

		if config.DockerfileBuild {
			if err = sc.BuildAndPush(ctx, config.InputFile, config.DestinationImages, regConfig, os.Stdout, os.Stderr); err != nil {
				return fmt.Errorf("failed to build image: %v", err)
			}
			return nil
		}

		if config.InputFile != "" {
			if err = sc.Upload(ctx, config.InputFile); err != nil {
				return err
			}
		}

		executor = sc.Executor(ctx)
		filesystem = &executorFS{executor: executor}

		if config.SourceImage != "" {
			// build/deploy/deploy-only is not required since this is an image deploy
			// all we need to do is return the inspected files and image and push the
			// destination images based on the sidecar container.

			if err = inspect(ctx, sc, config.SourceImage, filesystem, os.Stdout, os.Stderr); err != nil {
				return fmt.Errorf("error inspecting sidecar: %v", err)
			}

			if err = sc.TagAndPush(ctx, config.SourceImage, config.DestinationImages, regConfig, os.Stdout); err != nil {
				return fmt.Errorf("error pushing images: %v", err)
			}

			return nil
		}
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
		return err
	}

	if sc != nil {
		err = pushSidecar(ctx, sc, config, regConfig, os.Stdout)
		if err != nil {
			return fmt.Errorf("error in commit and push: %v", err)
		}
	}
	return nil
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
