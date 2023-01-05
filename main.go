// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/appdefaults"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/tsuru/deploy-agent/pkg/build"
	"github.com/tsuru/deploy-agent/pkg/build/buildkit"
	buildpb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
	"github.com/tsuru/deploy-agent/pkg/health"
)

const (
	DefaultServerMaxRecvMsgSize = 4 * (1 << 30) // 4 GiB
	DefaultServerMaxSendMsgSize = math.MaxInt32 // int32 max length
)

var cfg struct {
	BuildkitAddress      string
	BuildkitTmpDir       string
	Port                 int
	ServerMaxRecvMsgSize int
	ServerMaxSendMsgSize int
}

func main() {
	flag.IntVar(&cfg.Port, "port", 8080, "Server TCP port")
	flag.IntVar(&cfg.ServerMaxRecvMsgSize, "max-receiving-message-size", DefaultServerMaxRecvMsgSize, "Max message size in bytes that server can receive")
	flag.IntVar(&cfg.ServerMaxSendMsgSize, "max-sending-message-size", DefaultServerMaxSendMsgSize, "Max message size in bytes that server can send")

	flag.StringVar(&cfg.BuildkitAddress, "buildkit-addr", getEnvOrDefault("BUILDKIT_HOST", appdefaults.Address), "Buildkit server address")
	flag.StringVar(&cfg.BuildkitTmpDir, "buildkit-tmp-dir", os.TempDir(), "Directory path to store temp files during container image builds")
	flag.Parse()

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to listen: %v", err)
		os.Exit(1)
	}

	ctx := context.Background()

	c, err := client.New(ctx, cfg.BuildkitAddress, client.WithFailFast())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create Buildkit client: %v", err)
		os.Exit(1)
	}
	defer c.Close()

	serverOpts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(cfg.ServerMaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.ServerMaxSendMsgSize),
	}

	s := grpc.NewServer(serverOpts...)
	buildpb.RegisterBuildServer(s, build.NewServer(buildkit.NewBuildKit(c, buildkit.BuildKitOptions{TempDir: cfg.BuildkitTmpDir})))
	healthpb.RegisterHealthServer(s, health.NewServer())

	go handleGracefulTermination(s)

	fmt.Println("Starting gRPC server at", l.Addr().String())

	if err := s.Serve(l); err != nil {
		fmt.Fprintln(os.Stderr, "failed to run gRPC server:", err)
		os.Exit(1)
	}

	fmt.Println("gRPC server terminated")
}

func handleGracefulTermination(s *grpc.Server) {
	defer func() {
		fmt.Fprintln(os.Stdout, "Received termination signal. Terminating gRPC server...")
		s.GracefulStop()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

func getEnvOrDefault(env, def string) string {
	if envvar, found := os.LookupEnv(env); found {
		return envvar
	}

	return def
}
