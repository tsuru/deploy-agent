// Copyright 2023 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

var _ pb.BuildServer = (*Server)(nil)

func NewServer(b Builder) *Server {
	return &Server{b: b}
}

type Server struct {
	*pb.UnimplementedBuildServer
	b Builder
}

func (s *Server) Build(req *pb.BuildRequest, stream pb.Build_BuildServer) error {
	fmt.Println("Build RPC called")
	defer fmt.Println("Finishing Build RPC call")

	ctx := stream.Context()
	if err := ctx.Err(); err != nil { // e.g. context deadline exceeded
		return err
	}

	if err := validateBuildRequest(req); err != nil {
		return err
	}

	w := &BuildResponseOutputWriter{stream: stream}
	fmt.Fprintln(w, "---> Starting container image build")

	appFiles, err := s.b.Build(ctx, req, w)
	if err != nil {
		return err
	}

	if appFiles != nil {
		if err = stream.Send(&pb.BuildResponse{Data: &pb.BuildResponse_TsuruConfig{TsuruConfig: appFiles}}); err != nil {
			return status.Errorf(codes.Unknown, "failed to send tsuru app files: %s", err)
		}
	}

	fmt.Fprintln(w, "--> Container image build finished")

	return nil
}

func validateBuildRequest(r *pb.BuildRequest) error {
	if r == nil {
		return status.Error(codes.Internal, "build request cannot be nil")
	}

	if r.SourceImage == "" && r.Containerfile == "" {
		return status.Error(codes.InvalidArgument, "either source image or containerfile must be set")
	}

	if len(r.DestinationImages) == 0 {
		return status.Error(codes.InvalidArgument, "destination images not provided")
	}

	for _, dst := range r.DestinationImages {
		if dst == "" {
			return status.Error(codes.InvalidArgument, "destination image cannot be empty")
		}
	}

	kind, found := pb.BuildKind_name[int32(r.Kind)]
	if !found {
		return status.Error(codes.InvalidArgument, "invalid build kind")
	}

	if strings.HasPrefix(kind, "BUILD_KIND_APP_") && r.App == nil {
		return status.Error(codes.InvalidArgument, "app cannot be nil")
	}

	if strings.HasPrefix(kind, "BUILD_KIND_PLATFORM_") && r.Platform == nil {
		return status.Error(codes.InvalidArgument, "platform cannot be nil")
	}

	switch kind {
	case "BUILD_KIND_APP_BUILD_WITH_SOURCE_UPLOAD":
		if err := validateBuildRequestFromSourceData(r); err != nil {
			return err
		}

	case "BUILD_KIND_PLATFORM_WITH_CONTAINER_FILE":
		if err := validateBuildRequestFromContainerfile(r); err != nil {
			return err
		}
	}

	return nil
}

func validateBuildRequestFromSourceData(r *pb.BuildRequest) error {
	if r.SourceImage == "" {
		return status.Error(codes.InvalidArgument, "source image cannot be empty")
	}

	if len(r.Data) == 0 {
		return status.Error(codes.InvalidArgument, "app source data not provided")
	}

	return nil
}

func validateBuildRequestFromContainerfile(r *pb.BuildRequest) error {
	if r.Containerfile == "" {
		return status.Error(codes.InvalidArgument, "containerfile cannot be empty")
	}

	return nil
}
