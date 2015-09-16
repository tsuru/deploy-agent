package main

import (
	"fmt"
	"github.com/tsuru/tsuru/exec/exectest"
	"gopkg.in/check.v1"
)

func (s *S) TestExecScript(c *check.C) {
	cmds := []string{"ls", "ls"}
	envs := map[string]interface{}{
		"foo": "bar",
		"bar": 2,
	}
	err := execScript(cmds, envs)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("ls")
	c.Assert(len(executedCmds), check.Equals, 2)
	c.Assert(s.exec.ExecutedCmd("ls", nil), check.Equals, true)
}

func (s *S) TestExecScriptWithError(c *check.C) {
	cmds := []string{"not-exists"}
	osExecutor = &exectest.ErrorExecutor{}
	envs := map[string]interface{}{}
	err := execScript(cmds, envs)
	c.Assert(err, check.NotNil)
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
	envs := map[string]interface{}{
		"foo": "bar",
	}
	err := buildHooks(tsuruYaml, envs)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("/bin/bash -lc ls")
	c.Assert(len(executedCmds), check.Equals, 1)
	executedCmds = s.exec.GetCommands("/bin/bash -lc cd")
	c.Assert(len(executedCmds), check.Equals, 1)
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
