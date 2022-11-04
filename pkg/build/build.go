// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"context"
	"io"

	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

type Builder interface {
	Build(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error)
}
