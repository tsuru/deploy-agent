// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package user

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"

	"github.com/tsuru/tsuru/app/bind"
	"github.com/tsuru/tsuru/exec"
)

const (
	defaultUserIfRoot = "ubuntu"
	oldUserPrefix     = "tsuru.old."
	tsuruUIDEnv       = "TSURU_OS_UID"
)

var sudoFixTemplate *template.Template

func init() {
	sudoFixTemplate, _ = template.New("userfix").Parse(`
usermod -u {{.newUID}} {{.username}};
groupmod -g {{.newUID}} {{.username}};
useradd -M -U -u {{.oldUID}} {{.oldUserPrefix}}{{.username}};
echo "{{.oldUserPrefix}}{{.username}} ALL=(#{{.newUID}}) NOPASSWD:ALL" >>/etc/sudoers;
find / -mount -user {{.oldUID}} -exec chown -h {{.newUID}}:{{.newUID}} {} +;
`)
}

func ChangeUser(executor exec.Executor, envs []bind.EnvVar) (exec.Executor, error) {
	nameStdout := bytes.NewBuffer(nil)
	err := executor.Execute(exec.ExecuteOptions{
		Cmd:    "id",
		Args:   []string{"-un"},
		Stdout: nameStdout,
	})
	if err != nil {
		return nil, err
	}
	username := strings.TrimSpace(nameStdout.String())
	if username == "root" {
		username = defaultUserIfRoot
	}
	idStdout := bytes.NewBuffer(nil)
	err = executor.Execute(exec.ExecuteOptions{
		Cmd:    "id",
		Args:   []string{"-u"},
		Stdout: idStdout,
	})
	if err != nil {
		return nil, err
	}
	oldUID, _ := strconv.Atoi(strings.TrimSpace(idStdout.String()))
	newUID, _ := strconv.Atoi(strings.TrimSpace(getEnv(envs, tsuruUIDEnv)))
	if newUID == 0 ||
		oldUID == newUID {
		return executor, nil
	}
	newExecutor := &userExecutor{
		baseExecutor: executor,
		uid:          newUID,
	}
	if strings.HasPrefix(username, oldUserPrefix) {
		return newExecutor, nil
	}
	buf := bytes.NewBuffer(nil)
	err = sudoFixTemplate.Execute(buf, map[string]interface{}{
		"newUID":        newUID,
		"oldUID":        oldUID,
		"oldUserPrefix": oldUserPrefix,
		"username":      username,
	})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stdout, " ---> Converting user %d(%s) to UID %d from %s env\n", oldUID, username, newUID, tsuruUIDEnv)
	return newExecutor, executor.Execute(exec.ExecuteOptions{
		Cmd: "sudo",
		Args: []string{
			"--", "sh", "-c", buf.String(),
		},
		Dir:    "/",
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

type userExecutor struct {
	baseExecutor exec.Executor
	uid          int
}

func (e *userExecutor) Execute(opts exec.ExecuteOptions) error {
	if ue, ok := e.baseExecutor.(interface {
		ExecuteAsUser(string, exec.ExecuteOptions) error
	}); ok {
		uidStr := strconv.Itoa(e.uid)
		return ue.ExecuteAsUser(uidStr+":"+uidStr, opts)
	}
	return e.execute(opts)
}

func (e *userExecutor) IsRemote() bool {
	if isR, ok := e.baseExecutor.(interface {
		IsRemote() bool
	}); ok {
		return isR.IsRemote()
	}
	return false
}

func (e *userExecutor) execute(opts exec.ExecuteOptions) error {
	uidStr := fmt.Sprintf("#%d", e.uid)
	args := []string{
		"-u", uidStr, "-g", uidStr, "--", opts.Cmd,
	}
	opts.Args = append(args, opts.Args...)
	opts.Cmd = "sudo"
	return e.baseExecutor.Execute(opts)
}

func getEnv(envs []bind.EnvVar, key string) string {
	for _, e := range envs {
		if e.Name == key {
			return e.Value
		}
	}
	return ""
}
