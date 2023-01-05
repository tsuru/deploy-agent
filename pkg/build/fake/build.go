// Copyright 2023 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"context"
	"errors"
	"io"

	"github.com/tsuru/deploy-agent/pkg/build"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

var _ build.Builder = (*FakeBuilder)(nil)

type FakeBuilder struct {
	OnBuild func(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error)
}

func (b *FakeBuilder) Build(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error) {
	if b.OnBuild == nil {
		return nil, errors.New("fake: method not implemented")
	}

	return b.OnBuild(ctx, r, w)
}
