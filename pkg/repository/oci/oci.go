// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oci

import (
	"context"
	"fmt"
	"strings"

	"github.com/oracle/oci-go-sdk/v65/artifacts"
	"github.com/oracle/oci-go-sdk/v65/common"
)

type OCIRequiredMethods interface {
	CreateContainerRepository(ctx context.Context, request artifacts.CreateContainerRepositoryRequest) (response artifacts.CreateContainerRepositoryResponse, err error)
	ListContainerRepositories(ctx context.Context, request artifacts.ListContainerRepositoriesRequest) (response artifacts.ListContainerRepositoriesResponse, err error)
}

type OCI struct {
	client        OCIRequiredMethods
	CompartmentID string
	Profile       string
	ConfigPath    string
}

func NewOCI(data map[string]string) *OCI {
	return &OCI{
		CompartmentID: data["compartmentID"],
		Profile:       data["profile"],
		ConfigPath:    data["configPath"],
		client:        &artifacts.ArtifactsClient{},
	}
}

func (r *OCI) Ensure(ctx context.Context, name string) error {
	err := r.auth(ctx)
	if err != nil {
		return err
	}
	exists, err := r.exists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return r.create(ctx, name)
	}
	return nil
}

func (r *OCI) auth(ctx context.Context) error {
	if r.client != nil {
		return nil
	}
	configProvider := common.CustomProfileConfigProvider(r.ConfigPath, r.Profile)
	client, err := artifacts.NewArtifactsClientWithConfigurationProvider(configProvider)
	r.client = &client
	if err != nil {
		return err
	}
	return err
}

func (r *OCI) create(ctx context.Context, name string) error {
	name, err := parserRegistryRepository(name)
	if err != nil {
		return err
	}
	request := artifacts.CreateContainerRepositoryRequest{
		CreateContainerRepositoryDetails: artifacts.CreateContainerRepositoryDetails{
			CompartmentId: &r.CompartmentID,
			DisplayName:   common.String(name),
		},
	}
	_, err = r.client.CreateContainerRepository(ctx, request)
	if err != nil {
		return err
	}
	return nil
}

func (r *OCI) exists(ctx context.Context, name string) (bool, error) {
	name, err := parserRegistryRepository(name)
	if err != nil {
		return false, err
	}
	request := artifacts.ListContainerRepositoriesRequest{
		CompartmentId: &r.CompartmentID,
		DisplayName:   common.String(name),
	}
	response, err := r.client.ListContainerRepositories(ctx, request)
	if err != nil {
		return false, err
	}
	if len(response.ContainerRepositoryCollection.Items) == 0 {
		return false, nil
	}
	return true, nil
}

func parserRegistryRepository(image string) (string, error) {
	parts := strings.Split(image, "/")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid image format %s", image)
	}
	repoWithTag := strings.Join(parts[2:], "/")
	repoParts := strings.Split(repoWithTag, ":")
	repoWithoutTag := repoParts[0]

	return repoWithoutTag, nil
}
