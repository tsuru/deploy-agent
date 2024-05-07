// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"context"
	"io"

	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

const (
	DefaultTsuruPlatformWorkingDir = "/home/application/current"
	ProcfileName                   = "Procfile"
)

var (
	TsuruYamlNames  = []string{"tsuru.yml", "tsuru.yaml", "app.yml", "app.yaml"}
	TsuruConfigDirs = []string{DefaultTsuruPlatformWorkingDir, "/app/user", "/"}
)

type Builder interface {
	Build(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error)
}
