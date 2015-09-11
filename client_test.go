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

func (s *S) TestClient(c *check.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Header.Get("Authorization"), check.Not(check.Equals), "")
		c.Assert(r.URL.Path, check.Equals, "/apps/test/units/register")
		envs := map[string]interface{}{
			"foo": "bar",
		}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	cli := Client{
		URL:   server.URL,
		Token: "test-token",
	}
	_, err := cli.registerUnit("test", nil)
	c.Assert(err, check.IsNil)
}
