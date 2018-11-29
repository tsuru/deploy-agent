// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
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
	envs, err := c.RegisterUnit(appName, nil)
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
	rawYamlData, err := loadTsuruYamlRaw(fs)
	if err != nil {
		return err
	}
	yamlData, err := parseTsuruYaml(rawYamlData)
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
	fullYamlData, err := parseAllTsuruYaml(rawYamlData)
	if err != nil {
		return err
	}
	fullYamlData["processes"] = yamlData.Processes
	_, err = c.RegisterUnit(appName, fullYamlData)
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
		return nil, fmt.Errorf("failed to upload input file: %v", err)
	}
	return sideCar, nil
}

// pushSidecar commits the sidecar primary container, tags and pushes its image
func pushSidecar(dockerClient *docker.Client, sideCar *docker.Sidecar, config Config, w io.Writer) error {
	if len(config.DestinationImages) == 0 {
		return nil
	}
	fmt.Fprintln(w, "---- Building application image ----")
	imgID, err := sideCar.CommitPrimaryContainer(context.Background(), config.DestinationImages[0])
	if err != nil {
		return fmt.Errorf("failed to commit main container: %v", err)
	}
	return tagAndPushDestinations(dockerClient, imgID, config, w)
}

func tagAndPushDestinations(dockerClient *docker.Client, srcImgID string, config Config, w io.Writer) error {
	authConfig := docker.AuthConfig{
		Username:      config.RegistryAuthUser,
		Password:      config.RegistryAuthPass,
		Email:         config.RegistryAuthEmail,
		ServerAddress: config.RegistryAddress,
	}
	for _, destImg := range config.DestinationImages {
		if err := tagAndPush(dockerClient, srcImgID, destImg, authConfig, config.RegistryPushRetries, w); err != nil {
			return err
		}
	}
	return nil
}

func tagAndPush(dockerClient *docker.Client, imgID, imageName string, auth docker.AuthConfig, retries int, w io.Writer) error {
	img, err := dockerClient.Tag(context.Background(), imgID, imageName)
	if err != nil {
		return fmt.Errorf("error tagging image %v: %v", img, err)
	}
	return pushImage(dockerClient, img, auth, retries, w)
}

func inspect(dockerClient *docker.Client, image string, filesystem Filesystem, w io.Writer, errW io.Writer) error {
	imgInspect, err := dockerClient.Inspect(context.Background(), image)
	if err != nil {
		return fmt.Errorf("failed to inspect image %q: %v", image, err)
	}
	rawYamlData, err := loadTsuruYamlRaw(filesystem)
	if err != nil {
		return err
	}
	tsuruYaml, err := parseAllTsuruYaml(rawYamlData)
	if err != nil {
		return fmt.Errorf("failed to load tsuru yaml: %v", err)
	}
	procfileDirs := []string{defaultWorkingDir, "/app/user", ""}
	var procfile string
	for _, d := range procfileDirs {
		procfile, err = readProcfile(d, filesystem)
		if err != nil {
			// we can safely ignore this error since tsuru may use the image CMD/Entrypoint
			fmt.Fprintf(errW, "Unable to read procfile in %v: %v", d, err)
			continue
		}
		break
	}
	m := tsuru.InspectData{
		TsuruYaml: tsuruYaml,
		Image:     imgInspect,
		Procfile:  procfile,
	}
	err = json.NewEncoder(w).Encode(m)
	if err != nil {
		return fmt.Errorf("failed to encode inspected data %v: %v", m, err)
	}
	return nil
}

func buildAndPush(dockerClient *docker.Client, imageName string, fileName string, config Config, w io.Writer) error {
	auth := docker.AuthConfig{
		Username:      config.RegistryAuthUser,
		Password:      config.RegistryAuthPass,
		Email:         config.RegistryAuthEmail,
		ServerAddress: config.RegistryAddress,
	}
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open input file %q: %v", fileName, err)
	}
	defer file.Close()
	err = dockerClient.BuildImage(context.Background(), imageName, file)
	if err != nil {
		return fmt.Errorf("error building image %v: %v", imageName, err)
	}
	img := docker.ParseImageName(imageName)
	return pushImage(dockerClient, img, auth, config.RegistryPushRetries, w)
}

func pushImage(dockerClient *docker.Client, img docker.Image, auth docker.AuthConfig, retries int, w io.Writer) error {
	fmt.Fprintf(w, " ---> Sending image to repository (%s)\n", img)
	var err error
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
