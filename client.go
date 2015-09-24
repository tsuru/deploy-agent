// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/tsuru/tsuru/app/bind"
)

type Client struct {
	URL   string
	Token string
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
	u := fmt.Sprintf("%s/apps/%s/units/register", c.URL, appName)
	req, err := http.NewRequest("POST", u, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cli := &http.Client{}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	var envs []bind.EnvVar
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&envs)
	if err != nil {
		return nil, err
	}
	return envs, nil
}
