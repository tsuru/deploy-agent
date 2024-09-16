// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.4.0
// - protoc             v5.27.1
// source: pkg/build/grpc_build_v1/build_service.proto

package grpc_build_v1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.62.0 or later.
const _ = grpc.SupportPackageIsVersion8

const (
	Build_Build_FullMethodName = "/grpc_build_v1.Build/Build"
)

// BuildClient is the client API for Build service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type BuildClient interface {
	// Builds (and pushes) container images.
	Build(ctx context.Context, in *BuildRequest, opts ...grpc.CallOption) (Build_BuildClient, error)
}

type buildClient struct {
	cc grpc.ClientConnInterface
}

func NewBuildClient(cc grpc.ClientConnInterface) BuildClient {
	return &buildClient{cc}
}

func (c *buildClient) Build(ctx context.Context, in *BuildRequest, opts ...grpc.CallOption) (Build_BuildClient, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &Build_ServiceDesc.Streams[0], Build_Build_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &buildBuildClient{ClientStream: stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Build_BuildClient interface {
	Recv() (*BuildResponse, error)
	grpc.ClientStream
}

type buildBuildClient struct {
	grpc.ClientStream
}

func (x *buildBuildClient) Recv() (*BuildResponse, error) {
	m := new(BuildResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// BuildServer is the server API for Build service.
// All implementations must embed UnimplementedBuildServer
// for forward compatibility
type BuildServer interface {
	// Builds (and pushes) container images.
	Build(*BuildRequest, Build_BuildServer) error
	mustEmbedUnimplementedBuildServer()
}

// UnimplementedBuildServer must be embedded to have forward compatible implementations.
type UnimplementedBuildServer struct {
}

func (UnimplementedBuildServer) Build(*BuildRequest, Build_BuildServer) error {
	return status.Errorf(codes.Unimplemented, "method Build not implemented")
}
func (UnimplementedBuildServer) mustEmbedUnimplementedBuildServer() {}

// UnsafeBuildServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to BuildServer will
// result in compilation errors.
type UnsafeBuildServer interface {
	mustEmbedUnimplementedBuildServer()
}

func RegisterBuildServer(s grpc.ServiceRegistrar, srv BuildServer) {
	s.RegisterService(&Build_ServiceDesc, srv)
}

func _Build_Build_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(BuildRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(BuildServer).Build(m, &buildBuildServer{ServerStream: stream})
}

type Build_BuildServer interface {
	Send(*BuildResponse) error
	grpc.ServerStream
}

type buildBuildServer struct {
	grpc.ServerStream
}

func (x *buildBuildServer) Send(m *BuildResponse) error {
	return x.ServerStream.SendMsg(m)
}

// Build_ServiceDesc is the grpc.ServiceDesc for Build service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Build_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "grpc_build_v1.Build",
	HandlerType: (*BuildServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Build",
			Handler:       _Build_Build_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "pkg/build/grpc_build_v1/build_service.proto",
}
