// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"gopkg.in/check.v1"
	"net/http"
	"net/http/httptest"
)

func (s *S) TestDeploy(c *check.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Equals, "/apps/app1/units/register")
		envs := map[string]interface{}{
			"foo": "bar",
		}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	tsuruYmlData := `hooks:
  build:
    - ls
    - ls`
	f, err := s.fs.Create(fmt.Sprintf("%s/%s", workingDir, "tsuru.yml"))
	defer f.Close()
	c.Assert(err, check.IsNil)
	_, err = f.WriteString(tsuruYmlData)
	c.Assert(err, check.IsNil)
	args := []string{server.URL, "fake-token", "app1", "ls"}
	deployAgent(args)
}
