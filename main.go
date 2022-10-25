// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "github.com/tsuru/deploy-agent/v2/api/v1alpha1"
	"github.com/tsuru/deploy-agent/v2/pkg/build"
)

var cfg struct {
	Port int
}

func main() {
	flag.IntVar(&cfg.Port, "port", 4444, "Server TCP port")
	flag.Parse()

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to listen: %v", err)
		os.Exit(1)
	}

	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create Docker client: %v", err)
		os.Exit(1)
	}
	defer dc.Close()

	ctx, cancel := context.WithDeadline(context.TODO(), time.Now().Add(5*time.Second))
	defer cancel()

	_, err = dc.Ping(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to ping Docker API: %v", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	pb.RegisterBuildServer(s, build.NewDocker(dc, build.DockerOptions{}))

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
