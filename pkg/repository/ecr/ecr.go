// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ecr

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/config"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// <account-id>.dkr.ecr.<region>.amazonaws.com (with optional FIPS endpoint)
var ecrHostRegexp = regexp.MustCompile(`^\d{12}\.dkr\.ecr(?:-fips)?\.([a-z0-9-]+)\.amazonaws\.com$`)

type ECRRequiredMethods interface {
	CreateRepository(ctx context.Context, params *awsecr.CreateRepositoryInput, optFns ...func(*awsecr.Options)) (*awsecr.CreateRepositoryOutput, error)
}

type ECR struct {
	client ECRRequiredMethods
	Region string
	mu     sync.Mutex
}

func NewECR(data map[string]string) *ECR {
	return &ECR{
		Region: data["region"],
	}
}

func (r *ECR) Ensure(ctx context.Context, name string) error {
	repo, region, err := parseImage(name)
	if err != nil {
		return err
	}
	if r.Region != "" {
		region = r.Region
	}
	client, err := r.getClient(ctx, region)
	if err != nil {
		return err
	}
	_, err = client.CreateRepository(ctx, &awsecr.CreateRepositoryInput{
		RepositoryName: &repo,
	})
	var alreadyExists *types.RepositoryAlreadyExistsException
	if errors.As(err, &alreadyExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to create ECR repository %q: %w", repo, err)
	}
	return nil
}

// getClient lazily creates the AWS client; deploys run concurrently, so the
// client field is only accessed under the lock.
func (r *ECR) getClient(ctx context.Context, region string) (ECRRequiredMethods, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil {
		return r.client, nil
	}
	var optFns []func(*config.LoadOptions) error
	if region != "" {
		optFns = append(optFns, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, err
	}
	r.client = awsecr.NewFromConfig(cfg)
	return r.client, nil
}

// parseImage extracts the ECR repository name (the full path after the
// registry host, without tag or digest) and, when the host is a standard ECR
// endpoint, the AWS region embedded in it.
func parseImage(image string) (string, string, error) {
	host, repo, found := strings.Cut(image, "/")
	if !found || repo == "" {
		return "", "", fmt.Errorf("invalid image format %s", image)
	}
	if i := strings.Index(repo, "@"); i >= 0 {
		repo = repo[:i]
	}
	if i := strings.LastIndex(repo, ":"); i >= 0 {
		repo = repo[:i]
	}
	var region string
	if m := ecrHostRegexp.FindStringSubmatch(host); m != nil {
		region = m[1]
	}
	return repo, region, nil
}
