// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"gopkg.in/check.v1"
	"net/http"
	"net/http/httptest"
)

func (s *S) TestDeploy(c *check.C) {
	oldWorkingDir := workingDir
	workingDir = "/tmp"
	defer func() {
		workingDir = oldWorkingDir
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Equals, "/apps/app1/units/register")
		envs := map[string]interface{}{
			"foo": "bar",
		}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	args := []string{server.URL, "fake-token", "app1", "ls"}
	deployAgent(args)
}
