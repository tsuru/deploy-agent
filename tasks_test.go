package main

import (
	"fmt"
	"github.com/tsuru/tsuru/app/bind"
	"github.com/tsuru/tsuru/exec/exectest"
	"gopkg.in/check.v1"
)

func (s *S) TestExecScript(c *check.C) {
	cmds := []string{"ls", "ls"}
	envs := []bind.EnvVar{{
		Name:   "foo",
		Value:  "bar",
		Public: true,
	}, {
		Name:   "bar",
		Value:  "2",
		Public: true,
	}}
	err := execScript(cmds, envs)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("/bin/bash")
	c.Assert(len(executedCmds), check.Equals, 2)
	dir := executedCmds[0].GetDir()
	c.Assert(dir, check.Equals, workingDir)
	cmdEnvs := executedCmds[0].GetEnvs()
	c.Assert(cmdEnvs, check.DeepEquals, []string{"foo=bar", "bar=2"})
	args := executedCmds[0].GetArgs()
	expectedArgs := []string{"-lc", "ls"}
	c.Assert(args, check.DeepEquals, expectedArgs)
}

func (s *S) TestExecScriptWithError(c *check.C) {
	cmds := []string{"not-exists"}
	osExecutor = &exectest.ErrorExecutor{}
	err := execScript(cmds, nil)
	c.Assert(err, check.NotNil)
}

func (s *S) TestExecScriptWorkingDirNotExist(c *check.C) {
	err := s.fs.Remove(workingDir)
	c.Assert(err, check.IsNil)
	err = s.fs.Mkdir("/", 0777)
	c.Assert(err, check.IsNil)
	cmds := []string{"ls"}
	envs := []bind.EnvVar{{
		Name:   "foo",
		Value:  "bar",
		Public: true,
	}}
	err = execScript(cmds, envs)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("/bin/bash")
	c.Assert(len(executedCmds), check.Equals, 1)
	dir := executedCmds[0].GetDir()
	c.Assert(dir, check.Equals, "/")
}

func (s *S) TestLoadAppYaml(c *check.C) {
	tsuruYmlData := `hooks:
  build:
    - test
    - another_test`
	tsuruYmlPath := fmt.Sprintf("%s/%s", workingDir, "tsuru.yml")
	s.fs.FileContent = tsuruYmlData
	_, err := s.fs.Create(tsuruYmlPath)
	c.Assert(err, check.IsNil)
	c.Assert(s.fs.HasAction(fmt.Sprintf("create %s", tsuruYmlPath)), check.Equals, true)
	expected := TsuruYaml{
		Hooks: BuildHook{BuildHooks: []string{"test", "another_test"}},
	}
	t, err := loadTsuruYaml()
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestBuildHooks(c *check.C) {
	tsuruYaml := TsuruYaml{
		Hooks: BuildHook{BuildHooks: []string{"ls", "cd"}},
	}
	envs := []bind.EnvVar{{
		Name:   "foo",
		Value:  "bar",
		Public: true,
	}}
	err := buildHooks(tsuruYaml, envs)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("/bin/bash")
	c.Assert(len(executedCmds), check.Equals, 2)
	args := executedCmds[0].GetArgs()
	expectedArgs := []string{"-lc", "/bin/bash -lc ls"}
	c.Assert(args, check.DeepEquals, expectedArgs)
	args = executedCmds[1].GetArgs()
	expectedArgs = []string{"-lc", "/bin/bash -lc cd"}
	c.Assert(args, check.DeepEquals, expectedArgs)
}

func (s *S) TestLoadProcfile(c *check.C) {
	procfile := "web: python app.py"
	procfilePath := fmt.Sprintf("%s/%s", workingDir, "Procfile")
	s.fs.FileContent = procfile
	_, err := s.fs.Create(procfilePath)
	c.Assert(err, check.IsNil)
	c.Assert(s.fs.HasAction(fmt.Sprintf("create %s", procfilePath)), check.Equals, true)
	expected := TsuruYaml{
		Procfile: "web: python app.py",
	}
	t := TsuruYaml{}
	err = loadProcfile(&t)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestLoadMultilineProcfile(c *check.C) {
	procfile := `web: python app.py
worker: python worker.py`
	procfilePath := fmt.Sprintf("%s/%s", workingDir, "Procfile")
	s.fs.FileContent = procfile
	_, err := s.fs.Create(procfilePath)
	c.Assert(err, check.IsNil)
	c.Assert(s.fs.HasAction(fmt.Sprintf("create %s", procfilePath)), check.Equals, true)
	expected := TsuruYaml{
		Procfile: "web: python app.py\nworker: python worker.py",
	}
	t := TsuruYaml{}
	err = loadProcfile(&t)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestLoadProcess(c *check.C) {
	procfile := "web: python app.py"
	procfilePath := fmt.Sprintf("%s/%s", workingDir, "Procfile")
	s.fs.FileContent = procfile
	_, err := s.fs.Create(procfilePath)
	c.Assert(err, check.IsNil)
	c.Assert(s.fs.HasAction(fmt.Sprintf("create %s", procfilePath)), check.Equals, true)
	expected := TsuruYaml{
		Process: map[string]string{
			"web": "python app.py",
		},
	}
	t := TsuruYaml{}
	err = loadProcess(&t)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestLoadMultiProcess(c *check.C) {
	procfile := `web: python app.py
worker: run-task`
	procfilePath := fmt.Sprintf("%s/%s", workingDir, "Procfile")
	s.fs.FileContent = procfile
	_, err := s.fs.Create(procfilePath)
	c.Assert(err, check.IsNil)
	c.Assert(s.fs.HasAction(fmt.Sprintf("create %s", procfilePath)), check.Equals, true)
	expected := TsuruYaml{
		Process: map[string]string{
			"web":    "python app.py",
			"worker": "run-task",
		},
	}
	t := TsuruYaml{}
	err = loadProcess(&t)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}
