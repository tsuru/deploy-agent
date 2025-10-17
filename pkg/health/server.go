// Copyright 2025 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package health

import (
	"context"

	"google.golang.org/grpc/codes"
	pb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

var _ pb.HealthServer = (*Server)(nil)

func NewServer() *Server {
	return &Server{}
}

type Server struct {
	*pb.UnimplementedHealthServer
}

func (s *Server) Check(ctx context.Context, r *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &pb.HealthCheckResponse{
		Status: pb.HealthCheckResponse_SERVING,
	}, nil
}

func (s *Server) Watch(r *pb.HealthCheckRequest, w pb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "watch method not implemented")
}
