// Copyright 2017 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package user

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
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

var testProcessCurrentUser func(*user.User) = nil

func ChangeUser(executor exec.Executor, envs []bind.EnvVar) (exec.Executor, error) {
	user, err := user.Current()
	if err != nil {
		return nil, err
	}
	if testProcessCurrentUser != nil {
		testProcessCurrentUser(user)
	}
	username := user.Username
	if username == "" {
		username = user.Name
	}
	if username == "root" {
		username = defaultUserIfRoot
	}
	oldUID := os.Getuid()
	newUID, _ := strconv.ParseUint(getEnv(envs, tsuruUIDEnv), 10, 32)
	if newUID == 0 ||
		oldUID == int(newUID) ||
		user.Uid == string(newUID) {
		return executor, nil
	}
	newExecutor := &userExecutor{
		baseExecutor: executor,
		uid:          int(newUID),
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
	args := []string{
		"-u", fmt.Sprintf("#%d", e.uid), "--", opts.Cmd,
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
