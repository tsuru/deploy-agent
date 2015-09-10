package main

import (
	"testing"
)

func TestExecStartScript(t *testing.T) {
	workingDir = "/tmp"
	cmds := []string{"ls", "ls"}
	envs := map[string]interface{}{
		"foo": "bar",
		"bar": 2,
	}
	err := execStartScript(cmds, envs)
	if err != nil {
		t.Errorf("Got error %s", err)
	}
}

func TestExecStartScriptWithError(t *testing.T) {
	workingDir = "/tmp"
	cmds := []string{"not-exists"}
	envs := map[string]interface{}{}
	err := execStartScript(cmds, envs)
	if err == nil {
		t.Errorf("Expected error. Got nil")
	}
}
