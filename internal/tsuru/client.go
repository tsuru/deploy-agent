// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tsuru

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/tsuru/tsuru/app/bind"
)

type TsuruYaml struct {
	Hooks       Hook                   `json:"hooks,omitempty"`
	Processes   map[string]string      `json:"processes,omitempty"`
	Healthcheck map[string]interface{} `yaml:"healthcheck" json:"healthcheck,omitempty"`
}

type Hook struct {
	BuildHooks []string               `yaml:"build,omitempty" json:"build"`
	Restart    map[string]interface{} `yaml:"restart" json:"restart"`
}

func (t *TsuruYaml) IsEmpty() bool {
	return len(t.Hooks.BuildHooks) == 0 && t.Processes == nil
}

type Client struct {
	URL     string
	Token   string
	Version string
}

var httpClient = &http.Client{
	Transport: &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 30 * time.Second,
	},
	Timeout: time.Minute,
}

func (c Client) GetAppEnvs(appName string) ([]bind.EnvVar, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	v := url.Values{}
	v.Set("hostname", hostname)
	u := c.url(fmt.Sprintf("/apps/%s/env", appName))
	req, err := http.NewRequest("GET", u, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	var envs []bind.EnvVar
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &envs)
	if err != nil {
		return nil, fmt.Errorf("invalid response from tsuru API: %s", data)
	}
	return envs, nil
}

func (c Client) RegisterUnit(appName string, customData TsuruYaml) ([]bind.EnvVar, error) {
	var err error
	var yamlData []byte
	if !customData.IsEmpty() {
		yamlData, err = json.Marshal(customData)
		if err != nil {
			return nil, err
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	v := url.Values{}
	v.Set("hostname", hostname)
	v.Set("customdata", string(yamlData))
	u := c.url(fmt.Sprintf("/apps/%s/units/register", appName))
	req, err := http.NewRequest("POST", u, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	var envs []bind.EnvVar
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &envs)
	if err != nil {
		return nil, fmt.Errorf("invalid response from tsuru API: %s", data)
	}
	return envs, nil
}

func (c Client) SendDiffDeploy(diff, appName string) error {
	var err error
	v := url.Values{}
	v.Set("customdata", diff)
	u := c.url(fmt.Sprintf("/apps/%s/diff", appName))
	req, err := http.NewRequest("POST", u, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if string(data) != "" {
		fmt.Println(string(data))
	}
	return nil
}

func (c Client) url(path string) string {
	return fmt.Sprintf("%s/%s", strings.TrimRight(c.URL, "/"), strings.TrimLeft(path, "/"))
}

func (c Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Agent-Version", c.Version)
}
