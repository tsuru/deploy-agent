// Copyright 2023 tsuru authors. All rights reserved.
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
	"time"

	"github.com/moby/buildkit/client"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

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
	BuildkitAddress                                           string
	BuildkitTmpDir                                            string
	BuildKitAutoDiscoveryKubernetesPodSelector                string
	BuildKitAutoDiscoveryKubernetesNamespace                  string
	BuildKitAutoDiscoveryKubernetesLeasePrefix                string
	KubernetesConfig                                          string
	BuildKitAutoDiscoveryTimeout                              time.Duration
	BuildKitAutoDiscoveryKubernetesPort                       int
	Port                                                      int
	ServerMaxRecvMsgSize                                      int
	ServerMaxSendMsgSize                                      int
	BuildKitAutoDiscovery                                     bool
	BuildKitAutoDiscoveryKubernetesSetTsuruAppLabels          bool
	BuildKitAutoDiscoveryKubernetesUseSameNamespaceAsTsuruApp bool
}

func main() {
	klog.InitFlags(flag.CommandLine)

	flag.IntVar(&cfg.Port, "port", 8080, "Server TCP port")
	flag.IntVar(&cfg.ServerMaxRecvMsgSize, "max-receiving-message-size", DefaultServerMaxRecvMsgSize, "Max message size in bytes that server can receive")
	flag.IntVar(&cfg.ServerMaxSendMsgSize, "max-sending-message-size", DefaultServerMaxSendMsgSize, "Max message size in bytes that server can send")

	flag.StringVar(&cfg.KubernetesConfig, "kubeconfig", getEnvOrDefault("KUBECONFIG", ""), "Path to kubeconfig file")

	flag.StringVar(&cfg.BuildkitAddress, "buildkit-addr", getEnvOrDefault("BUILDKIT_HOST", ""), "Buildkit server address")
	flag.StringVar(&cfg.BuildkitTmpDir, "buildkit-tmp-dir", os.TempDir(), "Directory path to store temp files during container image builds")

	flag.BoolVar(&cfg.BuildKitAutoDiscovery, "buildkit-autodiscovery", false, "Whether should dynamically discover the BuildKit service based on Tsuru app (if any)")
	flag.DurationVar(&cfg.BuildKitAutoDiscoveryTimeout, "buildkit-autodiscovery-timeout", (5 * time.Minute), "Max duration to discover an available BuildKit")
	flag.StringVar(&cfg.BuildKitAutoDiscoveryKubernetesPodSelector, "buildkit-autodiscovery-kubernetes-pod-selector", "", "Label selector of BuildKit's pods on Kubernetes")
	flag.StringVar(&cfg.BuildKitAutoDiscoveryKubernetesNamespace, "buildkit-autodiscovery-kubernetes-namespace", "", "Namespace of BuildKit's pods on Kubernetes")
	flag.StringVar(&cfg.BuildKitAutoDiscoveryKubernetesLeasePrefix, "buildkit-autodiscovery-kubernetes-lease-prefix", "deploy-agent", "Prefix name for Lease resources")
	flag.IntVar(&cfg.BuildKitAutoDiscoveryKubernetesPort, "buildkit-autodiscovery-kubernetes-port", 80, "TCP port number which BuldKit's service is listening")
	flag.BoolVar(&cfg.BuildKitAutoDiscoveryKubernetesSetTsuruAppLabels, "buildkit-autodiscovery-kubernetes-set-tsuru-app-labels", false, "Whether should set the Tsuru app labels in the selected BuildKit pod")
	flag.BoolVar(&cfg.BuildKitAutoDiscoveryKubernetesUseSameNamespaceAsTsuruApp, "buildkit-autodiscovery-kubernetes-use-same-namespace-as-tsuru-app", false, "Whether should look for BuildKit in the Tsuru app's namespace")

	flag.Parse()

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to listen: %v", err)
		os.Exit(1)
	}

	bk, err := newBuildKit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create BuildKit: %v", err)
		os.Exit(1)
	}
	defer bk.Close()

	serverOpts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(cfg.ServerMaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.ServerMaxSendMsgSize),
	}

	s := grpc.NewServer(serverOpts...)
	buildpb.RegisterBuildServer(s, build.NewServer(bk))
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

func newBuildKit() (*buildkit.BuildKit, error) {
	opts := buildkit.BuildKitOptions{
		TempDir:                      cfg.BuildkitTmpDir,
		DiscoverBuildKitClientForApp: cfg.BuildKitAutoDiscovery,
	}

	b := buildkit.NewBuildKit(nil, opts)

	if cfg.BuildkitAddress != "" {
		c, err := client.New(context.Background(), cfg.BuildkitAddress, client.WithFailFast())
		if err != nil {
			return nil, fmt.Errorf("failed to create buildkit client: %w", err)
		}

		b = buildkit.NewBuildKit(c, opts)
	}

	if cfg.BuildKitAutoDiscovery {
		config, err := clientcmd.BuildConfigFromFlags("", cfg.KubernetesConfig)
		if err != nil {
			return nil, err
		}

		cs, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, err
		}

		dcs, err := dynamic.NewForConfig(config)
		if err != nil {
			return nil, err
		}

		kdopts := buildkit.KubernertesDiscoveryOptions{
			Timeout:               cfg.BuildKitAutoDiscoveryTimeout,
			PodSelector:           cfg.BuildKitAutoDiscoveryKubernetesPodSelector,
			Namespace:             cfg.BuildKitAutoDiscoveryKubernetesNamespace,
			Port:                  cfg.BuildKitAutoDiscoveryKubernetesPort,
			SetTsuruAppLabel:      cfg.BuildKitAutoDiscoveryKubernetesSetTsuruAppLabels,
			UseSameNamespaceAsApp: cfg.BuildKitAutoDiscoveryKubernetesUseSameNamespaceAsTsuruApp,
			LeasePrefix:           cfg.BuildKitAutoDiscoveryKubernetesLeasePrefix,
		}

		return b.WithKubernetesDiscovery(cs, dcs, kdopts), nil
	}

	return b, nil
}
