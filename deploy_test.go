// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeploy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/app1/units/register" {
			t.Errorf("URL should be /apps/app1/units/register. Got %s", r.URL.Path)
		}
		envs := map[string]interface{}{
			"foo": "bar",
		}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	args := []string{server.URL, "fake-token", "app1", "cmd"}
	deployAgent(args)
}
