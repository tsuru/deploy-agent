// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tsuru/deploy-agent/internal/docker"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/exec"
)

func build(c tsuru.Client, appName string, cmd []string, fs Filesystem, executor exec.Executor) error {
	envs, err := c.GetAppEnvs(appName)
	if err != nil {
		return err
	}
	return execScript(cmd, envs, os.Stdout, fs, executor)
}

func deploy(c tsuru.Client, appName string, fs Filesystem, executor exec.Executor) error {
	var yamlData tsuru.TsuruYaml
	envs, err := c.RegisterUnit(appName, yamlData)
	if err != nil {
		return err
	}
	diff, firstDeploy, err := readDiffDeploy(fs)
	if !firstDeploy && err == nil {
		err = c.SendDiffDeploy(diff, appName)
		if err != nil {
			return err
		}
	}
	yamlData, err = loadTsuruYaml(fs)
	if err != nil {
		return err
	}
	err = buildHooks(yamlData, envs, fs, executor)
	if err != nil {
		return err
	}
	err = loadProcesses(&yamlData, fs)
	if err != nil {
		return err
	}
	_, err = c.RegisterUnit(appName, yamlData)
	return err
}

// setupSidecar setups up a sidecar instance and uploads the input file to the primary container
func setupSidecar(dockerClient *docker.Client, config Config) (*docker.Sidecar, error) {
	sideCar, err := docker.NewSidecar(dockerClient, config.RunAsUser)
	if err != nil {
		return nil, fmt.Errorf("failed to create sidecar: %v", err)
	}
	if config.InputFile == "" {
		return sideCar, nil
	}
	err = sideCar.UploadToPrimaryContainer(context.Background(), config.InputFile)
	if err != nil {
		fatal("failed to upload input file: %v", err)
	}
	return sideCar, nil
}

// pushSidecar commits the sidecar primary container, tags and pushes its image
func pushSidecar(dockerClient *docker.Client, sideCar *docker.Sidecar, config Config, w io.Writer) error {
	fmt.Fprintln(w, "---- Building application image ----")
	img, err := sideCar.CommitPrimaryContainer(context.Background(), config.DestinationImage)
	if err != nil {
		return fmt.Errorf("failed to commit main container: %v", err)
	}
	authConfig := docker.AuthConfig{
		Username:      config.RegistryAuthUser,
		Password:      config.RegistryAuthPass,
		Email:         config.RegistryAuthEmail,
		ServerAddress: config.RegistryAddress,
	}
	if err := tagAndPush(dockerClient, img, authConfig, config.RegistryPushRetries, w); err != nil {
		return fmt.Errorf("Error pushing image: %v", err)
	}
	return nil
}

func tagAndPush(dockerClient *docker.Client, img docker.Image, auth docker.AuthConfig, retries int, w io.Writer) error {
	err := dockerClient.Tag(context.Background(), img)
	if err != nil {
		return fmt.Errorf("error tagging image %v: %v", img, err)
	}
	fmt.Fprintf(w, " ---> Sending image to repository (%s)\n", img)
	for i := 0; i < retries; i++ {
		err = dockerClient.Push(context.Background(), auth, img)
		if err != nil {
			fmt.Fprintf(w, "Could not send image, trying again. Original error: %v\n", err)
			time.Sleep(time.Second)
			continue
		}
		break
	}
	if err != nil {
		return fmt.Errorf("Error pushing image: %v", err)
	}
	return nil
}
