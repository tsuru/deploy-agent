// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/tsuru/deploy-agent/internal/tsuru"
	"github.com/tsuru/tsuru/app/bind"
	"gopkg.in/check.v1"
)

func (s *S) TestBuild(c *check.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Equals, "/apps/app1/env")
		envs := []bind.EnvVar{{
			Name:   "foo",
			Value:  "bar",
			Public: true,
		}}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	client := tsuru.Client{
		URL:   server.URL,
		Token: "fake-token",
	}
	build(client, "app1", []string{"ls"}, s.fs, s.exec)
}

func (s *S) TestDeploy(c *check.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps/app1/diff" {
			fmt.Fprint(w, "")
			return
		}
		c.Assert(r.URL.Path, check.Equals, "/apps/app1/units/register")
		envs := []bind.EnvVar{{
			Name:   "foo",
			Value:  "bar",
			Public: true,
		}}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	tsuruYmlData := `hooks:
  build:
    - ls
    - ls`
	f, err := s.fs.Create(fmt.Sprintf("%s/%s", defaultWorkingDir, "tsuru.yml"))
	c.Assert(err, check.IsNil)
	defer f.Close()
	diff, err := s.fs.Create(fmt.Sprintf("%s/%s", defaultWorkingDir, "diff"))
	c.Assert(err, check.IsNil)
	defer diff.Close()
	_, err = f.WriteString(tsuruYmlData)
	c.Assert(err, check.IsNil)
	_, err = diff.WriteString(`diff`)
	c.Assert(err, check.IsNil)
	procfileData := `web: run-app`
	p, err := s.fs.Create(fmt.Sprintf("%s/%s", defaultWorkingDir, "Procfile"))
	defer p.Close()
	c.Assert(err, check.IsNil)
	_, err = p.WriteString(procfileData)
	c.Assert(err, check.IsNil)
	client := tsuru.Client{
		URL:   server.URL,
		Token: "fake-token",
	}
	deploy(client, "app1", s.fs, s.exec)
}
