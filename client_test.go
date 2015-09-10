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

func TestClient(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("Authorization header is required.")
		}
		if r.URL.Path != "/apps/test/units/register" {
			t.Errorf("Expected /apps/test/units/register URL. Got %s", r.URL.Path)
		}
		envs := map[string]interface{}{
			"foo": "bar",
		}
		e, _ := json.Marshal(envs)
		w.Write(e)
	}))
	c := Client{
		URL:   s.URL,
		Token: "test-token",
	}
	_, err := c.registerUnit("test", nil)
	if err != nil {
		t.Errorf("Got error %s", err)
	}
}
