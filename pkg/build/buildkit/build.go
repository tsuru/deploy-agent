// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildkit

import (
	"context"
	"errors"
	"io"

	pb "github.com/tsuru/deploy-agent/api/v1alpha1"
	"github.com/tsuru/deploy-agent/pkg/build"
)

var _ build.Builder = (*BuildKit)(nil)

type BuildKit struct{}

func (b *BuildKit) Build(ctx context.Context, r *pb.BuildRequest, tc *pb.TsuruConfig, w io.Writer) error {
	return errors.New("not implemented yet")
}

func (b *BuildKit) FindTsuruAppFiles(ctx context.Context, r *pb.BuildRequest) (*pb.TsuruConfig, error) {
	return nil, errors.New("not implemented yet")
}
