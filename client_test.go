// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"github.com/tsuru/tsuru/app/bind"
	"gopkg.in/check.v1"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
)

func (s *S) TestClient(c *check.C) {
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call += 1
		c.Assert(r.Header.Get("Authorization"), check.Not(check.Equals), "")
		c.Assert(r.Header.Get("Content-Type"), check.Equals, "application/x-www-form-urlencoded")
		c.Assert(r.URL.Path, check.Equals, "/apps/test/units/register")
		b, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		val, err := url.ParseQuery(string(b))
		c.Assert(err, check.IsNil)
		hostname := val.Get("hostname")
		c.Assert(hostname, check.Not(check.Equals), "")
		if call == 1 {
			c.Assert(val.Get("customdata"), check.Equals, "")
		} else {
			customdata := val.Get("customdata")
			expected := "{\"hooks\":{\"build\":[\"ls\",\"ls\"]},\"process\":{\"web\":\"test\"},\"procfile\":\"web: test\"}"
			c.Assert(customdata, check.Equals, expected)
		}
		envs := []bind.EnvVar{{
			Name:   "foo",
			Value:  "bar",
			Public: true,
		}}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	cli := Client{
		URL:   server.URL,
		Token: "test-token",
	}
	_, err := cli.registerUnit("test", TsuruYaml{})
	c.Assert(err, check.IsNil)
	t := TsuruYaml{
		Hooks:    BuildHook{[]string{"ls", "ls"}},
		Process:  map[string]string{"web": "test"},
		Procfile: "web: test",
	}
	_, err = cli.registerUnit("test", t)
	c.Assert(err, check.IsNil)
}
