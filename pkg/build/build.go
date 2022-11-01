// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"context"
	"io"

	pb "github.com/tsuru/deploy-agent/api/v1alpha1"
)

type Builder interface {
	Build(ctx context.Context, r *pb.BuildRequest, tc *pb.TsuruConfig, w io.Writer) error
	FindTsuruAppFiles(ctx context.Context, r *pb.BuildRequest) (*pb.TsuruConfig, error)
}
