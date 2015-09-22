// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/tsuru/tsuru/app/bind"
)

type Client struct {
	URL   string
	Token string
}

func (c Client) registerUnit(appName string, customData TsuruYaml) ([]bind.EnvVar, error) {
	yamlData, err := json.Marshal(customData)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/apps/%s/units/register", c.URL, appName)
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	data := fmt.Sprintf("hostname=%s&customdata=%s", hostname, yamlData)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	req.Header.Set("Authorization", fmt.Sprintf("bearer %s", c.Token))
	if err != nil {
		return nil, err
	}
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
