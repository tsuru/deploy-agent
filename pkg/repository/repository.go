// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tsuru/deploy-agent/pkg/repository/fake"
	"github.com/tsuru/deploy-agent/pkg/repository/oci"
)

type Repository interface {
	Ensure(ctx context.Context, name string) error
}

type RemoteRepositoryProvider map[string]map[string]string

func repositoryProvider(providerType string, data map[string]string) (Repository, error) {
	switch providerType {
	case "oci":
		return oci.NewOCI(data), nil
	case "fake":
		return &fake.FakeRepository{}, nil
	default:
		return nil, fmt.Errorf("unknow repositoy provider: %s", providerType)
	}
}

func NewRemoteRepository(data []byte) (map[string]Repository, error) {
	var remoteRepositoryProvider RemoteRepositoryProvider
	err := json.Unmarshal(data, &remoteRepositoryProvider)
	if err != nil {
		return nil, err
	}
	var repositoryMap = make(map[string]Repository)
	for k, v := range remoteRepositoryProvider {
		if p, ok := v["provider"]; ok {
			provider, err := repositoryProvider(p, v)
			if err != nil {
				return nil, err
			}
			repositoryMap[k] = provider
			continue
		}
		return nil, fmt.Errorf("provider key not found in repository %s", k)
	}
	return repositoryMap, nil
}
