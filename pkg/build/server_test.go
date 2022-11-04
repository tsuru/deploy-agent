// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	. "github.com/tsuru/deploy-agent/pkg/build"
	"github.com/tsuru/deploy-agent/pkg/build/fake"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

func TestBuild(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		ctx     context.Context
		builder Builder
		req     *pb.BuildRequest
		assert  func(t *testing.T, stream pb.Build_BuildClient, err error)
	}{
		"w/ context canceled": {
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.TODO())
				cancel()
				return ctx
			}(),
			assert: func(t *testing.T, _ pb.Build_BuildClient, err error) {
				assert.Error(t, err)
				assert.EqualError(t, err, status.Error(codes.Canceled, "context canceled").Error())
			},
		},

		"missing source image": {
			req: &pb.BuildRequest{},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				_, _, err = readResponse(t, stream)
				assert.Error(t, err)
				assert.EqualError(t, err, status.Error(codes.InvalidArgument, "source image cannot be empty").Error())
			},
		},

		"missing destination images": {
			req: &pb.BuildRequest{
				SourceImage: "tsuru/scratch:latest",
			},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				_, _, err = readResponse(t, stream)
				assert.Error(t, err)
				assert.EqualError(t, err, status.Error(codes.InvalidArgument, "destination images not provided").Error())
			},
		},

		"destionation images w/ empty element": {
			req: &pb.BuildRequest{
				SourceImage:       "tsuru/scratch:latest",
				DestinationImages: []string{"registry.example.com/tsuru/app-my-app:v1", ""},
			},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				_, _, err = readResponse(t, stream)
				assert.Error(t, err)
				assert.EqualError(t, err, status.Error(codes.InvalidArgument, "destination image cannot be empty").Error())
			},
		},

		"unspecified deploy origin": {
			req: &pb.BuildRequest{
				SourceImage:       "tsuru/scratch:latest",
				DestinationImages: []string{"registry.example.com/tsuru/app-my-app:v1"},
			},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				_, _, err = readResponse(t, stream)
				assert.EqualError(t, err, status.Error(codes.InvalidArgument, "deploy origin must be provided").Error())
			},
		},

		"invalid deploy origin": {
			req: &pb.BuildRequest{
				SourceImage:       "tsuru/scratch:latest",
				DestinationImages: []string{"registry.example.com/tsuru/app-my-app:v1"},
				DeployOrigin:      pb.DeployOrigin(1000),
			},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				_, _, err = readResponse(t, stream)
				assert.EqualError(t, err, status.Error(codes.InvalidArgument, "invalid deploy origin").Error())
			},
		},

		"deploy from source code, empty app source data": {
			req: &pb.BuildRequest{
				SourceImage:       "tsuru/scratch:latest",
				DestinationImages: []string{"registry.example.com/tsuru/app-my-app:v1"},
				DeployOrigin:      pb.DeployOrigin_DEPLOY_ORIGIN_SOURCE_FILES,
			},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				_, _, err = readResponse(t, stream)
				assert.EqualError(t, err, status.Error(codes.InvalidArgument, "app source data not provided").Error())
			},
		},

		"when builder returns an error": {
			builder: &fake.FakeBuilder{
				OnBuild: func(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error) {
					return nil, errors.New("some error")
				},
			},
			req: &pb.BuildRequest{
				SourceImage:       "tsuru/scratch:latest",
				DestinationImages: []string{"registry.example.com/tsuru/app-my-app:v1"},
				DeployOrigin:      pb.DeployOrigin_DEPLOY_ORIGIN_CONTAINER_IMAGE,
			},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				_, _, err = readResponse(t, stream)
				assert.EqualError(t, err, status.Error(codes.Unknown, "some error").Error())
			},
		},

		"build successful": {
			builder: &fake.FakeBuilder{
				OnBuild: func(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error) {
					assert.NotNil(t, ctx)
					assert.NotNil(t, r)
					assert.NotNil(t, w)
					fmt.Fprintln(w, "--- EXECUTING BUILD ---")
					return &pb.TsuruConfig{
						Procfile:  "web: ./path/to/server.sh --addr :${PORT}",
						TsuruYaml: "healthcheck:\n  path: /healthz",
					}, nil
				},
			},
			req: &pb.BuildRequest{
				SourceImage:       "tsuru/scratch:latest",
				DestinationImages: []string{"registry.example.com/tsuru/app-my-app:v1"},
				DeployOrigin:      pb.DeployOrigin_DEPLOY_ORIGIN_SOURCE_FILES,
				Data:              []byte("fake data :P"),
			},
			assert: func(t *testing.T, stream pb.Build_BuildClient, err error) {
				require.NoError(t, err)
				require.NotNil(t, stream)
				tsuruConfig, output, err := readResponse(t, stream)
				require.NoError(t, err)
				require.NotNil(t, tsuruConfig)
				assert.Equal(t, &pb.TsuruConfig{Procfile: "web: ./path/to/server.sh --addr :${PORT}", TsuruYaml: "healthcheck:\n  path: /healthz"}, tsuruConfig)
				assert.Regexp(t, `(.*)--- EXECUTING BUILD ---(.*)`, output)
			},
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotNil(t, tt.assert, "assert function not provided")

			serverAddr := setupServer(t, NewServer(tt.builder))
			c := setupClient(t, serverAddr)

			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx
			}

			resp, err := c.Build(ctx, tt.req)
			tt.assert(t, resp, err)
		})
	}
}

func setupServer(t *testing.T, bs pb.BuildServer) string {
	t.Helper()

	l, err := net.Listen("unix", filepath.Join(t.TempDir(), "server.sock"))
	require.NoError(t, err)

	s := grpc.NewServer()
	t.Cleanup(func() { s.Stop() })

	pb.RegisterBuildServer(s, bs)

	go func() {
		nerr := s.Serve(l)
		require.NoError(t, nerr)
	}()

	return filepath.Join("unix://", l.Addr().String())
}

func setupClient(t *testing.T, address string) pb.BuildClient {
	t.Helper()

	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return pb.NewBuildClient(conn)
}

func readResponse(t *testing.T, stream pb.Build_BuildClient) (*pb.TsuruConfig, string, error) {
	t.Helper()

	var tc *pb.TsuruConfig
	var buffer bytes.Buffer

	for {
		r, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, "", err
		}

		switch r.Data.(type) {
		case *pb.BuildResponse_TsuruConfig:
			tc = r.GetTsuruConfig()

		case *pb.BuildResponse_Output:
			_, err = io.WriteString(&buffer, r.GetOutput())
			require.NoError(t, err)
		}
	}

	return tc, buffer.String(), nil
}
