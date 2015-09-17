// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"github.com/tsuru/tsuru/app/bind"
	"gopkg.in/check.v1"
	"net/http"
	"net/http/httptest"
)

func (s *S) TestClient(c *check.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Header.Get("Authorization"), check.Not(check.Equals), "")
		c.Assert(r.URL.Path, check.Equals, "/apps/test/units/register")
		envs := []bind.EnvVar{{
			Name:   "foo",
			Value:  "bar",
			Public: true,
		}}
		e, _ := json.Marshal(envs)
		if customData := r.URL.Query().Get("customdata"); customData != "" {
			var tsuruCustomData TsuruYaml
			err := json.Unmarshal([]byte(customData), &tsuruCustomData)
			c.Assert(err, check.IsNil)
		}
		w.Write(e)
	}))
	cli := Client{
		URL:   server.URL,
		Token: "test-token",
	}
	_, err := cli.registerUnit("test", TsuruYaml{})
	c.Assert(err, check.IsNil)
}
