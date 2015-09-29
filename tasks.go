package main

import (
	"fmt"
	"github.com/tsuru/tsuru/app/bind"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/fs"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
)

var (
	defaultWorkingDir = "/home/application/current"
	tsuruYamlFiles    = []string{"tsuru.yml", "tsuru.yaml", "app.yml", "app.yaml"}
	appEnvsFile       = "/tmp/app_envs"
)

var fsystem fs.Fs

func filesystem() fs.Fs {
	if fsystem == nil {
		fsystem = &fs.OsFs{}
	}
	return fsystem
}

var osExecutor exec.Executor

func executor() exec.Executor {
	if osExecutor == nil {
		return &exec.OsExecutor{}
	}
	return osExecutor
}
func execScript(cmds []string, envs []bind.EnvVar) error {
	workingDir := defaultWorkingDir
	if _, err := filesystem().Stat(defaultWorkingDir); err != nil {
		if os.IsNotExist(err) {
			workingDir = "/"
		} else {
			return err
		}
	}
	formatedEnvs := []string{}
	for _, env := range envs {
		formatedEnv := fmt.Sprintf("%s=%s", env.Name, env.Value)
		formatedEnvs = append(formatedEnvs, formatedEnv)
	}
	errors := make(chan error, len(cmds))
	for _, cmd := range cmds {
		execOpts := exec.ExecuteOptions{
			Cmd:    "/bin/bash",
			Args:   []string{"-lc", cmd},
			Dir:    workingDir,
			Envs:   formatedEnvs,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
		err := executor().Execute(execOpts)
		if err != nil {
			errors <- err
		}
	}
	close(errors)
	formatedErrors := ""
	for e := range errors {
		formatedErrors += fmt.Sprintf("%s\n", e)
	}
	if formatedErrors != "" {
		return fmt.Errorf("%s", formatedErrors)
	}
	return nil
}

type TsuruYaml struct {
	Hooks    Hook              `json:"hooks,omitempty"`
	Process  map[string]string `json:"process,omitempty"`
	Procfile string            `json:"procfile,omitempty"`
}

type Hook struct {
	BuildHooks []string               `yaml:"build,omitempty" json:"build"`
	Restart    map[string]interface{} `yaml:"restart" json:"restart"`
}

func (t *TsuruYaml) isEmpty() bool {
	if len(t.Hooks.BuildHooks) == 0 && t.Process == nil && t.Procfile == "" {
		return true
	}
	return false
}
func loadTsuruYaml() (TsuruYaml, error) {
	var tsuruYamlData TsuruYaml
	for _, yamlFile := range tsuruYamlFiles {
		filePath := fmt.Sprintf("%s/%s", defaultWorkingDir, yamlFile)
		f, err := filesystem().Open(filePath)
		if err != nil {
			continue
		}
		defer f.Close()
		tsuruYaml, err := ioutil.ReadAll(f)
		if err != nil {
			return TsuruYaml{}, err
		}
		err = yaml.Unmarshal(tsuruYaml, &tsuruYamlData)
		if err != nil {
			return TsuruYaml{}, err
		}
		break
	}
	return tsuruYamlData, nil
}

func buildHooks(yamlData TsuruYaml, envs []bind.EnvVar) error {
	var cmds []string
	for _, cmd := range yamlData.Hooks.BuildHooks {
		cmds = append(cmds, cmd)
	}
	return execScript(cmds, envs)
}

func readProcfile() (string, error) {
	procfilePath := fmt.Sprintf("%s/%s", defaultWorkingDir, "Procfile")
	f, err := filesystem().Open(procfilePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	procfile, err := ioutil.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(procfile), nil
}

func loadProcfile(t *TsuruYaml) error {
	procfile, err := readProcfile()
	if err != nil {
		return err
	}
	t.Procfile = procfile
	return nil
}

var procfileRegex = regexp.MustCompile("^([A-Za-z0-9_]+):\\s*(.+)$")

func loadProcess(t *TsuruYaml) error {
	procfile, err := readProcfile()
	if err != nil {
		return err
	}
	process := map[string]string{}
	processes := strings.Split(procfile, "\n")
	for _, proc := range processes {
		if p := procfileRegex.FindStringSubmatch(proc); p != nil {
			process[p[1]] = strings.Trim(p[2], " ")
		}
	}
	t.Process = process
	return nil
}

func saveAppEnvsFile(envs []bind.EnvVar) error {
	f, err := filesystem().Create(appEnvsFile)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, e := range envs {
		f.Write([]byte(fmt.Sprintf("export %s='%s'\n", e.Name, e.Value)))
	}
	return nil
}
