// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	URL   string
	Token string
}

func (c Client) registerUnit(appName string, customData map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/apps/%s/units/register", c.URL, appName)
	req, err := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", fmt.Sprintf("bearer %s", c.Token))
	if err != nil {
		return nil, err
	}
	cli := &http.Client{}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	var envs map[string]interface{}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&envs)
	if err != nil {
		println(err.Error())
		return nil, err
	}
	return envs, nil
}
