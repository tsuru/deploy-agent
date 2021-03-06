// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"os"
	"syscall"

	"github.com/tsuru/deploy-agent/internal/tsuru"
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
	buf := bytes.NewBufferString("")
	err := execScript(cmds, envs, buf, s.fs, s.exec)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("/bin/sh")
	c.Assert(len(executedCmds), check.Equals, 2)
	dir := executedCmds[0].GetDir()
	c.Assert(dir, check.Equals, defaultWorkingDir)
	cmdEnvs := executedCmds[0].GetEnvs()
	expectedEnvs := []string{"foo=bar", "bar=2"}
	expectedEnvs = append(expectedEnvs, os.Environ()...)
	c.Assert(cmdEnvs, check.DeepEquals, expectedEnvs)
	args := executedCmds[0].GetArgs()
	expectedArgs := []string{"-lc", "ls"}
	c.Assert(args, check.DeepEquals, expectedArgs)
	c.Assert(buf.String(), check.Equals, " ---> Running \"ls\"\n ---> Running \"ls\"\n")
}

func (s *S) TestExecScriptWithError(c *check.C) {
	cmds := []string{"not-exists"}
	err := execScript(cmds, nil, nil, s.fs, &exectest.ErrorExecutor{})
	c.Assert(err, check.NotNil)
}

func (s *S) TestExecScriptWorkingDirNotExist(c *check.C) {
	err := s.fs.Remove(defaultWorkingDir)
	c.Assert(err, check.IsNil)
	err = s.fs.Mkdir("/", 0777)
	c.Assert(err, check.IsNil)
	cmds := []string{"ls"}
	envs := []bind.EnvVar{{
		Name:   "foo",
		Value:  "bar",
		Public: true,
	}}
	err = execScript(cmds, envs, nil, s.fs, s.exec)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("/bin/sh")
	c.Assert(len(executedCmds), check.Equals, 1)
	dir := executedCmds[0].GetDir()
	c.Assert(dir, check.Equals, "/")
}

func (s *S) TestLoadTsuruYamlRaw(c *check.C) {
	tsuruYmlData := `hooks:
  build:
    - test
    - another_test
  restart:
    before:
      - static
healthcheck:
  path: /test
  method: GET
  status: 200
  match: .*OK
  allowed_failures: 0
unknown: ok`
	tsuruYmlPath := fmt.Sprintf("%s/%s", defaultWorkingDir, "tsuru.yml")
	s.testFS().FileContent = tsuruYmlData
	_, err := s.fs.Create(tsuruYmlPath)
	c.Assert(err, check.IsNil)
	c.Assert(s.testFS().HasAction(fmt.Sprintf("create %s", tsuruYmlPath)), check.Equals, true)
	raw := loadTsuruYamlRaw(s.fs)
	c.Assert(string(raw), check.DeepEquals, tsuruYmlData)
}

func (s *S) TestLoadTsuruYamlRawNotFound(c *check.C) {
	raw := loadTsuruYamlRaw(s.fs)
	c.Assert(raw, check.IsNil)
}

func (s *S) TestParseAppYaml(c *check.C) {
	tsuruYmlData := `hooks:
  build:
    - test
    - another_test
  restart:
    before:
      - static
healthcheck:
  path: /test
  method: GET
  status: 200
  match: .*OK
  allowed_failures: 0
unknown_field: ignored`
	expected := tsuru.TsuruYaml{
		Hooks: tsuru.Hook{
			BuildHooks: []string{"test", "another_test"},
			Restart: map[string]interface{}{
				"before": []interface{}{"static"},
			},
		},
		Healthcheck: map[string]interface{}{
			"path":             "/test",
			"method":           "GET",
			"status":           float64(200),
			"match":            ".*OK",
			"allowed_failures": float64(0),
		},
	}
	t, err := parseTsuruYaml([]byte(tsuruYmlData))
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestParseAllAppYaml(c *check.C) {
	tsuruYmlData := `hooks:
  build:
    - test
    - another_test
  restart:
    before:
      - static
healthcheck:
  path: /test
  method: GET
  status: 200
  match: .*OK
  allowed_failures: 0
unknown_field: mapped`
	expected := map[string]interface{}{
		"hooks": map[string]interface{}{
			"build": []interface{}{"test", "another_test"},
			"restart": map[string]interface{}{
				"before": []interface{}{"static"},
			},
		},
		"healthcheck": map[string]interface{}{
			"path":             "/test",
			"method":           "GET",
			"status":           float64(200),
			"match":            ".*OK",
			"allowed_failures": float64(0),
		},
		"unknown_field": "mapped",
	}
	t, err := parseAllTsuruYaml([]byte(tsuruYmlData))
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestHooks(c *check.C) {
	tsuruYaml := tsuru.TsuruYaml{
		Hooks: tsuru.Hook{BuildHooks: []string{"ls", "cd"}},
	}
	envs := []bind.EnvVar{{
		Name:   "foo",
		Value:  "bar",
		Public: true,
	}}
	err := buildHooks(tsuruYaml, envs, s.fs, s.exec)
	c.Assert(err, check.IsNil)
	executedCmds := s.exec.GetCommands("/bin/sh")
	c.Assert(len(executedCmds), check.Equals, 2)
	args := executedCmds[0].GetArgs()
	expectedArgs := []string{"-lc", "ls"}
	c.Assert(args, check.DeepEquals, expectedArgs)
	args = executedCmds[1].GetArgs()
	expectedArgs = []string{"-lc", "cd"}
	c.Assert(args, check.DeepEquals, expectedArgs)
}

func (s *S) TestLoadProcesses(c *check.C) {
	procfile := "web: python app.py"
	procfilePath := fmt.Sprintf("%s/%s", defaultWorkingDir, "Procfile")
	s.testFS().FileContent = procfile
	_, err := s.fs.Create(procfilePath)
	c.Assert(err, check.IsNil)
	c.Assert(s.testFS().HasAction(fmt.Sprintf("create %s", procfilePath)), check.Equals, true)
	expected := tsuru.TsuruYaml{
		Processes: map[string]string{
			"web": "python app.py",
		},
	}
	t := tsuru.TsuruYaml{}
	err = loadProcesses(&t, s.fs)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestLoadMultiProcesses(c *check.C) {
	procfile := `web: python app.py
# worker: run-task
another-worker: run-task
# disabled-worker: run-task
`
	procfilePath := fmt.Sprintf("%s/%s", defaultWorkingDir, "Procfile")
	s.testFS().FileContent = procfile
	_, err := s.fs.Create(procfilePath)
	c.Assert(err, check.IsNil)
	c.Assert(s.testFS().HasAction(fmt.Sprintf("create %s", procfilePath)), check.Equals, true)
	expected := tsuru.TsuruYaml{
		Processes: map[string]string{
			"web":            "python app.py",
			"another-worker": "run-task",
		},
	}
	t := tsuru.TsuruYaml{}
	err = loadProcesses(&t, s.fs)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}

func (s *S) TestDontLoadWrongProcfile(c *check.C) {
	procfile := `web:
	@python test.py`
	procfilePath := fmt.Sprintf("%s/%s", defaultWorkingDir, "Procfile")
	s.testFS().FileContent = procfile
	_, err := s.fs.Create(procfilePath)
	c.Assert(err, check.IsNil)
	c.Assert(s.testFS().HasAction(fmt.Sprintf("create %s", procfilePath)), check.Equals, true)
	t := tsuru.TsuruYaml{}
	err = loadProcesses(&t, s.fs)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Equals, `invalid Procfile, no processes found in "web:\n\t@python test.py"`)
}

func (s *S) TestDiffDeploy(c *check.C) {
	diff := `--- hello.go	2015-11-25 16:04:22.409241045 +0000
+++ hello.go	2015-11-18 18:40:21.385697080 +0000
@@ -1,10 +1,7 @@
 package main

-import (
-    "fmt"
-)
+import "fmt"

-func main() {
-	fmt.Println("Hello")
+func main2() {
+	fmt.Println("Hello World!")
 }
`
	diffPath := fmt.Sprintf("%s/%s", defaultWorkingDir, "diff")
	s.testFS().FileContent = diff
	_, err := s.fs.Create(diffPath)
	c.Assert(err, check.IsNil)
	c.Assert(s.testFS().HasAction(fmt.Sprintf("create %s", diffPath)), check.Equals, true)
	result, first, err := readDiffDeploy(s.fs)
	c.Assert(err, check.IsNil)
	c.Assert(result, check.DeepEquals, diff)
	c.Assert(first, check.Equals, false)
}

func (s *S) TestReadProcfileNotFound(c *check.C) {
	_, err := readProcfile(s.fs)
	_, ok := err.(syscall.Errno)
	c.Assert(ok, check.Equals, true)
}

func (s *S) TestReadProcfileMultiplePaths(c *check.C) {
	tests := []struct {
		path string
	}{
		{
			path: "/Procfile",
		},
		{
			path: "/app/user/Procfile",
		},
		{
			path: "/home/application/current/Procfile",
		},
	}
	for i, tt := range tests {
		c.Log("test", i)
		procfile, err := s.fs.Create(tt.path)
		c.Assert(err, check.IsNil)
		_, err = procfile.Write([]byte(fmt.Sprintf("web: a-%d", i)))
		c.Assert(err, check.IsNil)
		result, err := readProcfile(s.fs)
		c.Assert(err, check.IsNil)
		c.Assert(result, check.Equals, fmt.Sprintf("web: a-%d", i))
	}
}

func (s *S) TestReadProcfileFound(c *check.C) {
	expected := "web: ls\naxl: \"echo Guns N' Roses\""
	procfileContent := expected
	procfile, err := s.fs.Create("/Procfile")
	c.Assert(err, check.IsNil)
	_, err = procfile.Write([]byte(procfileContent))
	c.Assert(err, check.IsNil)
	c.Assert(procfile.Close(), check.IsNil)
	result, err := readProcfile(s.fs)
	c.Assert(err, check.IsNil)
	c.Assert(result, check.Equals, expected)
}

func (s *S) TestReadProcfileNormalizeCRLFToLF(c *check.C) {
	procfileContent := "web: ls\r\nslash: \"echo Guns N' Roses\""
	expected := "web: ls\nslash: \"echo Guns N' Roses\""
	procfile, err := s.fs.Create("/Procfile")
	c.Assert(err, check.IsNil)
	_, err = procfile.Write([]byte(procfileContent))
	c.Assert(err, check.IsNil)
	c.Assert(procfile.Close(), check.IsNil)
	result, err := readProcfile(s.fs)
	c.Assert(err, check.IsNil)
	c.Assert(result, check.Equals, expected)
}

func (s *S) TestParseTsuruYamlEmpty(c *check.C) {
	t, err := parseTsuruYaml(nil)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, tsuru.TsuruYaml{})
}

func (s *S) TestParseAllTsuruYamlEmpty(c *check.C) {
	t, err := parseAllTsuruYaml(nil)
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, map[string]interface{}{})
}

func (s *S) TestLoadTsuruYamlRawMultiplePaths(c *check.C) {
	tests := []struct {
		path string
	}{
		{
			path: "/app.yaml",
		},
		{
			path: "/app/user/app.yaml",
		},
		{
			path: "/home/application/current/app.yaml",
		},
		{
			path: "/app.yml",
		},
		{
			path: "/app/user/app.yml",
		},
		{
			path: "/home/application/current/app.yml",
		},
		{
			path: "/tsuru.yaml",
		},
		{
			path: "/app/user/tsuru.yaml",
		},
		{
			path: "/home/application/current/tsuru.yaml",
		},
		{
			path: "/tsuru.yml",
		},
		{
			path: "/app/user/tsuru.yml",
		},
		{
			path: "/home/application/current/tsuru.yml",
		},
	}
	for i, tt := range tests {
		c.Log("test", i)
		config, err := s.fs.Create(tt.path)
		c.Assert(err, check.IsNil)
		_, err = config.Write([]byte(fmt.Sprintf("test: a-%d", i)))
		c.Assert(err, check.IsNil)
		result := loadTsuruYamlRaw(s.fs)
		c.Assert(string(result), check.Equals, fmt.Sprintf("test: a-%d", i))
	}
}
