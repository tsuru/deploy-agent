// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"context"
	"errors"
	"io"

	pb "github.com/tsuru/deploy-agent/api/v1alpha1"
	"github.com/tsuru/deploy-agent/pkg/build"
)

var _ build.Builder = (*FakeBuilder)(nil)

type FakeBuilder struct {
	OnBuild             func(ctx context.Context, r *pb.BuildRequest, tc *pb.TsuruConfig, w io.Writer) error
	OnFindTsuruAppFiles func(ctx context.Context, r *pb.BuildRequest) (*pb.TsuruConfig, error)
}

func (b *FakeBuilder) Build(ctx context.Context, r *pb.BuildRequest, tc *pb.TsuruConfig, w io.Writer) error {
	if b.OnBuild == nil {
		return errors.New("fake: method not implemented")
	}

	return b.OnBuild(ctx, r, tc, w)
}

func (b *FakeBuilder) FindTsuruAppFiles(ctx context.Context, r *pb.BuildRequest) (*pb.TsuruConfig, error) {
	if b.OnFindTsuruAppFiles == nil {
		return nil, errors.New("fake: method not implemented")
	}

	return b.OnFindTsuruAppFiles(ctx, r)
}
