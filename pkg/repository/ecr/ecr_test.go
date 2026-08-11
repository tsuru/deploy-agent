// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ecr

import (
	"context"
	"errors"
	"sync"
	"testing"

	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/stretchr/testify/assert"
)

type FakeECRClient struct {
	repos       map[string]bool
	failWith    error
	createCalls int
}

func (m *FakeECRClient) CreateRepository(ctx context.Context, request *awsecr.CreateRepositoryInput, optFns ...func(*awsecr.Options)) (*awsecr.CreateRepositoryOutput, error) {
	m.createCalls++
	if m.failWith != nil {
		return nil, m.failWith
	}
	repo := *request.RepositoryName
	if m.repos[repo] {
		return nil, &types.RepositoryAlreadyExistsException{}
	}
	if m.repos == nil {
		m.repos = make(map[string]bool)
	}
	m.repos[repo] = true
	return &awsecr.CreateRepositoryOutput{}, nil
}

func TestEnsureCreatesRepository(t *testing.T) {
	client := &FakeECRClient{}
	r := &ECR{client: client}
	err := r.Ensure(context.TODO(), "123456789012.dkr.ecr.us-east-1.amazonaws.com/tsuru/app-hello-world:v1")
	assert.NoError(t, err)
	assert.True(t, client.repos["tsuru/app-hello-world"])
}

func TestEnsureKeepsFullRepositoryPath(t *testing.T) {
	client := &FakeECRClient{}
	r := &ECR{client: client}
	err := r.Ensure(context.TODO(), "123456789012.dkr.ecr.us-east-1.amazonaws.com/org/sub/tsuru/app-hello-world:v1")
	assert.NoError(t, err)
	assert.True(t, client.repos["org/sub/tsuru/app-hello-world"])
}

func TestEnsureRepositoryAlreadyExists(t *testing.T) {
	client := &FakeECRClient{repos: map[string]bool{"tsuru/app-hello-world": true}}
	r := &ECR{client: client}
	err := r.Ensure(context.TODO(), "123456789012.dkr.ecr.us-east-1.amazonaws.com/tsuru/app-hello-world:v2")
	assert.NoError(t, err)
	assert.Equal(t, 1, client.createCalls)
}

func TestEnsureCreateRepositoryError(t *testing.T) {
	client := &FakeECRClient{failWith: errors.New("AccessDeniedException: not authorized to perform: ecr:CreateRepository")}
	r := &ECR{client: client}
	err := r.Ensure(context.TODO(), "123456789012.dkr.ecr.us-east-1.amazonaws.com/tsuru/app-hello-world:v1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tsuru/app-hello-world")
	assert.Contains(t, err.Error(), "AccessDeniedException")
}

func TestEnsureInvalidImage(t *testing.T) {
	client := &FakeECRClient{}
	r := &ECR{client: client}
	err := r.Ensure(context.TODO(), "app-hello-world")
	assert.Error(t, err)
	assert.Equal(t, 0, client.createCalls)
}

func TestGetClientConcurrent(t *testing.T) {
	r := NewECR(map[string]string{"region": "us-east-1"})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.getClient(context.TODO(), "us-east-1")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

func TestNewECR(t *testing.T) {
	r := NewECR(map[string]string{"region": "sa-east-1"})
	assert.Equal(t, "sa-east-1", r.Region)
}

func TestParseImage(t *testing.T) {
	tests := []struct {
		image  string
		repo   string
		region string
		err    bool
	}{
		{image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/tsuru/app-x:v1", repo: "tsuru/app-x", region: "us-east-1"},
		{image: "123456789012.dkr.ecr.sa-east-1.amazonaws.com/tsuru/app-x", repo: "tsuru/app-x", region: "sa-east-1"},
		{image: "123456789012.dkr.ecr-fips.us-gov-west-1.amazonaws.com/tsuru/app-x:latest", repo: "tsuru/app-x", region: "us-gov-west-1"},
		{image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/org/sub/tsuru/app-x:v10", repo: "org/sub/tsuru/app-x", region: "us-east-1"},
		{image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/tsuru/app-x@sha256:0000000000000000000000000000000000000000000000000000000000000000", repo: "tsuru/app-x", region: "us-east-1"},
		{image: "registry.example.com/tsuru/app-x:v1", repo: "tsuru/app-x", region: ""},
		{image: "app-hello-world", err: true},
	}
	for _, tt := range tests {
		repo, region, err := parseImage(tt.image)
		if tt.err {
			assert.Error(t, err, tt.image)
			continue
		}
		assert.NoError(t, err, tt.image)
		assert.Equal(t, tt.repo, repo, tt.image)
		assert.Equal(t, tt.region, region, tt.image)
	}
}
