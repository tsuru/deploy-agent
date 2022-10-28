// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/dockerfile/builder"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	tsuruprovtypes "github.com/tsuru/tsuru/types/provision"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	pb "github.com/tsuru/deploy-agent/v2/api/v1alpha1"
)

var _ pb.BuildServer = (*Docker)(nil)

type DockerOptions struct {
	TempDir string
}

func NewDocker(c *client.Client, opts DockerOptions) *Docker {
	return &Docker{cli: c, opts: opts}
}

type Docker struct {
	*pb.UnimplementedBuildServer

	cli  *client.Client
	opts DockerOptions
}

func (d *Docker) Build(req *pb.BuildRequest, stream pb.Build_BuildServer) error {
	fmt.Println("Build RPC called")
	defer fmt.Println("Finishing Build RPC call")

	ctx := stream.Context()
	if err := ctx.Err(); err != nil { // e.g. context deadline exceeded
		return err
	}

	w := &BuildResponseOutputWriter{stream: stream}
	fmt.Fprintln(w, "---> Starting container image build")

	// TODO: check if mandatory field were provided
	tsuruFiles, err := ExtractTsuruAppFilesFromAppSourceContext(ctx, bytes.NewReader(req.Data))
	if err != nil {
		return err
	}

	if err = stream.Send(&pb.BuildResponse{
		Data: &pb.BuildResponse_TsuruConfig{
			TsuruConfig: &pb.TsuruConfig{
				Procfile:  tsuruFiles.Procfile,
				TsuruYaml: tsuruFiles.TsuruYaml,
			},
		}}); err != nil {
		return status.Errorf(codes.Unknown, "failed to send tsuru app files: %s", err)
	}

	if err = d.build(ctx, req, tsuruFiles, bytes.NewReader(req.Data), w); err != nil {
		return status.Errorf(codes.Internal, "failed to build container image: %s", err)
	}

	fmt.Fprintln(w, "--> Container image build finished")

	return nil
}

func (d *Docker) build(ctx context.Context, req *pb.BuildRequest, tsuruAppFiles *TsuruAppFiles, appData io.Reader, w *BuildResponseOutputWriter) error {
	if err := ctx.Err(); err != nil { // e.g. context deadline exceeded
		return err
	}

	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, d.opts.TempDir, req, tsuruAppFiles, appData)
	if err != nil {
		return err
	}
	defer cleanFunc()

	pw, err := progresswriter.NewPrinter(context.Background(), w, "plain") // using an empty context intentionally
	if err != nil {
		return err
	}

	eg, _ := errgroup.WithContext(ctx)

	eg.Go(func() error {
		secrets, err := secretsprovider.NewStore([]secretsprovider.Source{{
			ID:       "tsuru-app-envvars",
			FilePath: filepath.Join(tmpDir, "envs.sh"),
		}})
		if err != nil {
			return err
		}

		var (
			insecureRegistry bool        // disabled by default
			pushImage        bool = true // enabled by default
		)
		if pots := req.PushOptions; pots != nil {
			pushImage = !pots.Disable
			insecureRegistry = pots.InsecureRegistry
		}

		opts := client.SolveOpt{
			LocalDirs: map[string]string{
				"context":    tmpDir,
				"dockerfile": tmpDir,
			},
			Exports: []client.ExportEntry{
				{
					Type: client.ExporterImage,
					Attrs: map[string]string{
						"name":              strings.Join(req.DestinationImages, ","),
						"push":              strconv.FormatBool(pushImage),
						"registry.insecure": strconv.FormatBool(insecureRegistry),
					},
				},
			},
			Session: []session.Attachable{
				authprovider.NewDockerAuthProvider(w),
				secretsprovider.NewSecretProvider(secrets),
			},
		}
		_, err = d.cli.Build(ctx, opts, "deploy-agent", builder.Build, progresswriter.ResetTime(pw).Status())
		return err
	})

	eg.Go(func() error {
		<-pw.Done()
		return pw.Err()
	})

	if err = eg.Wait(); err != nil {
		return err
	}

	return nil
}

func generateBuildLocalDir(ctx context.Context, baseDir string, req *pb.BuildRequest, tsuruAppFiles *TsuruAppFiles, appData io.Reader) (string, func(), error) {
	noopFunc := func() {}

	if err := ctx.Err(); err != nil {
		return "", noopFunc, err
	}

	contextRootDir, err := os.MkdirTemp(baseDir, "deploy-agent-*")
	if err != nil {
		return "", noopFunc, status.Errorf(codes.Internal, "failed to create temp dir: %s", err)
	}

	eg, _ := errgroup.WithContext(ctx)

	eg.Go(func() error {
		dockerfile, err := os.Create(filepath.Join(contextRootDir, "Dockerfile"))
		if err != nil {
			return status.Errorf(codes.Internal, "cannot create Dockerfile in %s: %s", contextRootDir, err)
		}
		defer dockerfile.Close()

		return generateContainerfile(dockerfile, req.SourceImage, tsuruAppFiles)
	})

	eg.Go(func() error {
		appArchive, err := os.Create(filepath.Join(contextRootDir, "application.tar.gz"))
		if err != nil {
			return status.Errorf(codes.Internal, "cannot create application archive: %s", err)
		}
		defer appArchive.Close()

		_, err = io.Copy(appArchive, appData)
		return err
	})

	eg.Go(func() error {
		envsFile, err := os.Create(filepath.Join(contextRootDir, "envs.sh"))
		if err != nil {
			return err
		}
		defer envsFile.Close()

		fmt.Fprintln(envsFile, "# File containing the env vars of Tsuru app. Generated by deploy-agent.")

		if req.App == nil {
			return nil
		}

		for k, v := range req.App.EnvVars {
			fmt.Fprintln(envsFile, fmt.Sprintf("%s=%q", k, v))
		}

		return nil
	})

	if err = eg.Wait(); err != nil {
		return "", noopFunc, err
	}

	return contextRootDir, func() { os.RemoveAll(contextRootDir) }, nil
}

func generateContainerfile(w io.Writer, image string, tsuruAppFiles *TsuruAppFiles) error {
	var tsuruYaml tsuruprovtypes.TsuruYamlData
	if tsuruAppFiles != nil {
		if err := yaml.Unmarshal([]byte(tsuruAppFiles.TsuruYaml), &tsuruYaml); err != nil {
			return err
		}
	}

	var buildHooks []string
	if hooks := tsuruYaml.Hooks; hooks != nil {
		buildHooks = hooks.Build
	}

	dockerfile, err := BuildContainerfile(BuildContainerfileParams{
		Image:      image,
		BuildHooks: buildHooks,
	})
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, dockerfile)
	return err
}

type BuildResponseOutputWriter struct {
	stream pb.Build_BuildServer
}

func (w *BuildResponseOutputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	return len(p), w.stream.Send(&pb.BuildResponse{Data: &pb.BuildResponse_Output{Output: string(p)}})
}

func (w *BuildResponseOutputWriter) Read(p []byte) (int, error) { // required to implement console.File
	return 0, nil
}

func (w *BuildResponseOutputWriter) Close() error { // required to implement console.File
	return nil
}

func (w *BuildResponseOutputWriter) Fd() uintptr { // required to implement console.File
	return uintptr(0)
}

func (w *BuildResponseOutputWriter) Name() string { // required to implement console.File
	return ""
}
