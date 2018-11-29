// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tsuru

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/tsuru/tsuru/app/bind"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type S struct{}

var _ = check.Suite(&S{})

func (s *S) TestClient(c *check.C) {
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		c.Assert(r.Header.Get("Authorization"), check.Not(check.Equals), "")
		c.Assert(r.Header.Get("Content-Type"), check.Equals, "application/x-www-form-urlencoded")
		c.Assert(r.Header.Get("X-Agent-Version"), check.Equals, "0.2.1")
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
			expected := "{\"hooks\":{\"build\":[\"ls\",\"ls\"]},\"processes\":{\"web\":\"test\"}}"
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
		URL:     server.URL,
		Token:   "test-token",
		Version: "0.2.1",
	}
	_, err := cli.RegisterUnit("test", nil)
	c.Assert(err, check.IsNil)
	t := map[string]interface{}{
		"hooks": map[string]interface{}{
			"build": []string{"ls", "ls"},
		},
		"processes": map[string]string{"web": "test"},
	}
	_, err = cli.RegisterUnit("test", t)
	c.Assert(err, check.IsNil)
}

func (s *S) TestClientSendDiff(c *check.C) {
	diff := `--- hello.go	2015-11-25 16:04:22.409241045 +0000
+++ hello.go	2015-11-18 18:40:21.385697080 +0000
@@ -1,10 +1,7 @@
 package main

-import (
-    "fmt"
-)
+import "fmt"

-func main() {
-	fmt.Println("Hello")
+func main2() {
+	fmt.Println("Hello World!")
 }
`
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		c.Assert(r.Header.Get("Authorization"), check.Not(check.Equals), "")
		c.Assert(r.Header.Get("Content-Type"), check.Equals, "application/x-www-form-urlencoded")
		c.Assert(r.Header.Get("X-Agent-Version"), check.Equals, "0.2.1")
		c.Assert(r.URL.Path, check.Equals, "/apps/test/diff")
		b, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		val, err := url.ParseQuery(string(b))
		c.Assert(err, check.IsNil)
		if call == 1 {
			c.Assert(val.Get("customdata"), check.Equals, "")
		} else {
			customdata := val.Get("customdata")
			c.Assert(customdata, check.Equals, diff)
		}
	}))
	cli := Client{
		URL:     server.URL,
		Token:   "test-token",
		Version: "0.2.1",
	}
	err := cli.SendDiffDeploy("", "test")
	c.Assert(err, check.IsNil)
	err = cli.SendDiffDeploy(diff, "test")
	c.Assert(err, check.IsNil)
}

func (s *S) TestClientUrl(c *check.C) {
	var tests = []struct {
		input    string
		expected string
	}{
		{"/", "http://localhost/"},
		{"/index", "http://localhost/index"},
		{"///index", "http://localhost/index"},
		{"/v1/register", "http://localhost/v1/register"},
		{"v1/register", "http://localhost/v1/register"},
	}
	cli := Client{URL: "http://localhost/", Token: "test-token"}
	for _, test := range tests {
		got := cli.url(test.input)
		c.Check(got, check.Equals, test.expected)
	}
}
