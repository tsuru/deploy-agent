// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oci

import (
	"context"
	"errors"
	"testing"

	"github.com/oracle/oci-go-sdk/v65/artifacts"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/stretchr/testify/assert"
)

type FakeArtifactsClient struct {
	repo map[string]bool
	artifacts.ArtifactsClient
}

func (m *FakeArtifactsClient) CreateContainerRepository(ctx context.Context, request artifacts.CreateContainerRepositoryRequest) (response artifacts.CreateContainerRepositoryResponse, err error) {
	repo := *request.CreateContainerRepositoryDetails.DisplayName
	if _, ok := m.repo[repo]; ok {
		return artifacts.CreateContainerRepositoryResponse{}, errors.New("repository already exists")
	}
	if m.repo == nil {
		m.repo = make(map[string]bool)
	}
	m.repo[repo] = true
	return artifacts.CreateContainerRepositoryResponse{}, nil
}

func (m *FakeArtifactsClient) ListContainerRepositories(ctx context.Context, request artifacts.ListContainerRepositoriesRequest) (response artifacts.ListContainerRepositoriesResponse, err error) {
	repos := make([]artifacts.ContainerRepositorySummary, 0, 10)
	for repo := range m.repo {
		repos = append(repos, artifacts.ContainerRepositorySummary{DisplayName: common.String(repo)})
	}
	return artifacts.ListContainerRepositoriesResponse{
		ContainerRepositoryCollection: artifacts.ContainerRepositoryCollection{
			Items: repos,
		},
	}, nil
}

func TestOCI_Create(t *testing.T) {
	fakeClient := new(FakeArtifactsClient)
	oci := &OCI{
		client: fakeClient,
	}
	ctx := context.TODO()
	name := "registry/namespace/test-repo"
	err := oci.Create(ctx, name)
	assert.NoError(t, err)
	err = oci.Create(ctx, name)
	assert.Error(t, err, "repository already exists")
}

func TestOCI_Exists(t *testing.T) {
	fakeClient := new(FakeArtifactsClient)
	oci := &OCI{
		client: fakeClient,
	}
	ctx := context.TODO()
	name := "registry/namespace/test-repo"
	exists, err := oci.Exists(ctx, name)
	assert.NoError(t, err)
	assert.False(t, exists)
	err = oci.Create(ctx, name)
	assert.NoError(t, err)
	exists, err = oci.Exists(ctx, name)
	assert.NoError(t, err)
	assert.True(t, exists)
}
func TestParserRegistryRepository(t *testing.T) {
	tests := []struct {
		name        string
		image       string
		expected    string
		expectedErr bool
	}{
		{
			name:        "No slash",
			image:       "image",
			expected:    "",
			expectedErr: true,
		},
		{
			name:        "One slash",
			image:       "registry/image",
			expected:    "",
			expectedErr: true,
		},
		{
			name:        "Multiple slashes",
			image:       "registry/namespace/image",
			expected:    "image",
			expectedErr: false,
		},
		{
			name:        "With tag",
			image:       "registry/namespace/image:tag",
			expected:    "image",
			expectedErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parserRegistryRepository(tt.image)
			if tt.expectedErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
