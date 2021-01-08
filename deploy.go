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

	"github.com/tsuru/deploy-agent/internal/sidecar"
	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/deploy-agent/internal/user"
	"github.com/tsuru/tsuru/exec"
)

func build(c tsuru.Client, appName string, cmd []string, fs Filesystem, executor exec.Executor) error {
	envs, err := c.GetAppEnvs(appName)
	if err != nil {
		return err
	}
	newExecutor, err := user.ChangeUser(executor, envs)
	if err != nil {
		return err
	}
	updateFSExecutor(fs, newExecutor)
	return execScript(cmd, envs, os.Stdout, fs, newExecutor)
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
	rawYamlData := loadTsuruYamlRaw(fs)
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

// pushSidecar commits the sidecar primary container, tags and pushes its image
func pushSidecar(ctx context.Context, sc sidecar.Sidecar, config Config, regConfig sidecar.RegistryConfig, w io.Writer) error {
	if len(config.DestinationImages) == 0 {
		return nil
	}
	fmt.Fprintln(w, "---- Building application image ----")
	_, err := sc.Commit(ctx, config.DestinationImages[0])
	if err != nil {
		return fmt.Errorf("failed to commit main container: %v", err)
	}
	fmt.Fprintln(w, "---- Pushing application image ----")
	return sc.TagAndPush(ctx, config.DestinationImages[0], config.DestinationImages, regConfig, w)
}

func inspect(ctx context.Context, sc sidecar.Sidecar, image string, filesystem Filesystem, w io.Writer, errW io.Writer) error {
	imgInspect, err := sc.Inspect(ctx, image)
	if err != nil {
		return fmt.Errorf("failed to inspect image %q: %v", image, err)
	}
	rawYamlData := loadTsuruYamlRaw(filesystem)
	if err != nil {
		return err
	}
	tsuruYaml, err := parseAllTsuruYaml(rawYamlData)
	if err != nil {
		return fmt.Errorf("failed to load tsuru yaml: %v", err)
	}
	procfile, err := readProcfile(filesystem)
	if err != nil {
		// we can safely ignore this error since tsuru may use the image CMD/Entrypoint
		fmt.Fprintf(errW, "Unable to read procfile: %v", err)
	}
	m := tsuru.InspectData{
		TsuruYaml: tsuruYaml,
		Image:     *imgInspect,
		Procfile:  procfile,
	}
	err = json.NewEncoder(w).Encode(m)
	if err != nil {
		return fmt.Errorf("failed to encode inspected data %v: %v", m, err)
	}
	return nil
}
