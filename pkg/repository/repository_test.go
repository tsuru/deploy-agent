// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tsuru/deploy-agent/pkg/repository/ecr"
	"github.com/tsuru/deploy-agent/pkg/repository/fake"
	"github.com/tsuru/deploy-agent/pkg/repository/oci"
)

func TestNewRemoteRepository(t *testing.T) {
	data := []byte(`{
	"test.com": {
				"provider": "oci",
				"compartmentID": "123",
				"profile": "dev"
	},
	"faker.com": {
				"provider": "fake"
	},
	"123456789012.dkr.ecr.us-east-1.amazonaws.com": {
				"provider": "ecr",
				"region": "us-east-1"
	}
	}`)
	repositoryMap, err := NewRemoteRepository(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assert.Len(t, repositoryMap, 3)
	assert.Equal(t, oci.NewOCI(map[string]string{"compartmentID": "123", "profile": "dev"}), repositoryMap["test.com"])
	assert.Equal(t, &fake.FakeRepository{}, repositoryMap["faker.com"].(*fake.FakeRepository))
	assert.Equal(t, ecr.NewECR(map[string]string{"provider": "ecr", "region": "us-east-1"}), repositoryMap["123456789012.dkr.ecr.us-east-1.amazonaws.com"])
}

func TestNewRepositoryInvalidProvider(t *testing.T) {
	data := []byte(`{
	"test.com": {
				"provider": "invalid"
	}
	}`)
	_, err := NewRemoteRepository(data)
	assert.Error(t, err)
	assert.Equal(t, "unknow repositoy provider: invalid", err.Error())
}
