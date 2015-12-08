// Copyright 2015 deploy-agent authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

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

type Client struct {
	URL   string
	Token string
}

var httpClient = &http.Client{
	Transport: &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	},
	Timeout: time.Minute,
}

func (c Client) registerUnit(appName string, customData TsuruYaml) ([]bind.EnvVar, error) {
	var err error
	var yamlData []byte
	if !customData.isEmpty() {
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
	req.Header.Set("Authorization", fmt.Sprintf("bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func (c Client) sendDiffDeploy(diff, appName string) error {
	var err error
	v := url.Values{}
	v.Set("customdata", diff)
	u := c.url(fmt.Sprintf("/apps/%s/diff", appName))
	req, err := http.NewRequest("POST", u, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
