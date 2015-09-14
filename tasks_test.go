package main

import (
	"fmt"
	"github.com/tsuru/tsuru/fs/fstest"
	"gopkg.in/check.v1"
)

func (s *S) TestExecScript(c *check.C) {
	oldWorkingDir := workingDir
	workingDir = "/tmp"
	defer func() {
		workingDir = oldWorkingDir
	}()
	cmds := []string{"ls", "ls"}
	envs := map[string]interface{}{
		"foo": "bar",
		"bar": 2,
	}
	err := execScript(cmds, envs)
	c.Assert(err, check.IsNil)
}

func (s *S) TestExecScriptWithError(c *check.C) {
	cmds := []string{"not-exists"}
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
	expected := map[string]interface{}{
		"hooks": map[interface{}]interface{}{
			"build": []interface{}{"test", "another_test"},
		},
	}
	t, err := loadTsuruYaml()
	c.Assert(err, check.IsNil)
	c.Assert(t, check.DeepEquals, expected)
}
