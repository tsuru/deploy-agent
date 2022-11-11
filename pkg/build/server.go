// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"fmt"

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

	if err = stream.Send(&pb.BuildResponse{Data: &pb.BuildResponse_TsuruConfig{TsuruConfig: appFiles}}); err != nil {
		return status.Errorf(codes.Unknown, "failed to send tsuru app files: %s", err)
	}

	fmt.Fprintln(w, "--> Container image build finished")

	return nil
}

func validateBuildRequest(r *pb.BuildRequest) error {
	if r == nil {
		return status.Error(codes.Internal, "build request cannot be nil")
	}

	if r.SourceImage == "" {
		return status.Error(codes.InvalidArgument, "source image cannot be empty")
	}

	if len(r.DestinationImages) == 0 {
		return status.Error(codes.InvalidArgument, "destination images not provided")
	}

	for _, dst := range r.DestinationImages {
		if dst == "" {
			return status.Error(codes.InvalidArgument, "destination image cannot be empty")
		}
	}

	if _, found := pb.DeployOrigin_name[int32(r.DeployOrigin)]; !found {
		return status.Error(codes.InvalidArgument, "invalid deploy origin")
	}

	switch pb.DeployOrigin_name[int32(r.DeployOrigin)] {
	case "DEPLOY_ORIGIN_SOURCE_FILES":
		if err := validateBuildRequestFromSourceData(r); err != nil {
			return err
		}
	}

	return nil
}

func validateBuildRequestFromSourceData(r *pb.BuildRequest) error {
	if len(r.Data) == 0 {
		return status.Error(codes.InvalidArgument, "app source data not provided")
	}

	return nil
}
