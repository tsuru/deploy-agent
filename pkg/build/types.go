// Copyright 2025 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// NOTE: Copied from github.com/tsuru/tsuru/types/provision/provision.go in
// order to avoid circular dependency with github.com/tsuru/tsuru module.

package build

import "maps"

type TsuruYamlData struct {
	Hooks        *TsuruYamlHooks            `json:"hooks,omitempty" bson:",omitempty"`
	Healthcheck  *TsuruYamlHealthcheck      `json:"healthcheck,omitempty" bson:",omitempty"`
	Startupcheck *TsuruYamlStartupcheck     `json:"startupcheck,omitempty" bson:",omitempty"`
	Kubernetes   *TsuruYamlKubernetesConfig `json:"kubernetes,omitempty" bson:",omitempty"`
	Processes    []TsuruYamlProcess         `json:"processes,omitempty" bson:",omitempty"`
}

type TsuruYamlHooks struct {
	Restart TsuruYamlRestartHooks `json:"restart" bson:",omitempty"`
	Build   []string              `json:"build" bson:",omitempty"`
}

type TsuruYamlRestartHooks struct {
	Before []string `json:"before" bson:",omitempty"`
	After  []string `json:"after" bson:",omitempty"`
}

type TsuruYamlHealthcheck struct {
	Headers              map[string]string `json:"headers,omitempty" bson:",omitempty"`
	Path                 string            `json:"path"`
	Scheme               string            `json:"scheme"`
	Command              []string          `json:"command,omitempty" bson:",omitempty"`
	AllowedFailures      int               `json:"allowed_failures,omitempty" yaml:"allowed_failures" bson:"allowed_failures,omitempty"`
	IntervalSeconds      int               `json:"interval_seconds,omitempty" yaml:"interval_seconds" bson:"interval_seconds,omitempty"`
	TimeoutSeconds       int               `json:"timeout_seconds,omitempty" yaml:"timeout_seconds" bson:"timeout_seconds,omitempty"`
	DeployTimeoutSeconds int               `json:"deploy_timeout_seconds,omitempty" yaml:"deploy_timeout_seconds" bson:"deploy_timeout_seconds,omitempty"`
	ForceRestart         bool              `json:"force_restart,omitempty" yaml:"force_restart" bson:"force_restart,omitempty"`
}

type TsuruYamlStartupcheck struct {
	Headers         map[string]string `json:"headers,omitempty" bson:",omitempty"`
	Path            string            `json:"path"`
	Scheme          string            `json:"scheme"`
	Command         []string          `json:"command,omitempty" bson:",omitempty"`
	AllowedFailures int               `json:"allowed_failures,omitempty" yaml:"allowed_failures" bson:"allowed_failures,omitempty"`
	IntervalSeconds int               `json:"interval_seconds,omitempty" yaml:"interval_seconds" bson:"interval_seconds,omitempty"`
	TimeoutSeconds  int               `json:"timeout_seconds,omitempty" yaml:"timeout_seconds" bson:"timeout_seconds,omitempty"`
}

type TsuruYamlProcess struct {
	Healthcheck  *TsuruYamlHealthcheck  `json:"healthcheck,omitempty" bson:",omitempty"`
	Startupcheck *TsuruYamlStartupcheck `json:"startupcheck,omitempty" bson:",omitempty"`
	Name         string                 `json:"name"`
	Command      string                 `json:"command" yaml:"command" bson:"command"`
}

type TsuruYamlKubernetesConfig struct {
	Groups map[string]TsuruYamlKubernetesGroup `json:"groups,omitempty"`
}

func (in *TsuruYamlKubernetesConfig) DeepCopyInto(out *TsuruYamlKubernetesConfig) {
	if in.Groups == nil {
		return
	}
	if out.Groups == nil {
		out.Groups = make(map[string]TsuruYamlKubernetesGroup)
	}
	maps.Copy(out.Groups, in.Groups)
}

func (in *TsuruYamlKubernetesConfig) DeepCopy() *TsuruYamlKubernetesConfig {
	out := &TsuruYamlKubernetesConfig{}
	in.DeepCopyInto(out)
	return out
}

type TsuruYamlKubernetesGroup map[string]TsuruYamlKubernetesProcessConfig

type TsuruYamlKubernetesProcessConfig struct {
	Ports []TsuruYamlKubernetesProcessPortConfig `json:"ports"`
}

type TsuruYamlKubernetesProcessPortConfig struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Port       int    `json:"port,omitempty"`
	TargetPort int    `json:"target_port,omitempty"`
}
