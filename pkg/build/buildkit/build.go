// Copyright 2023 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/containerd/console"
	containerregistryauthn "github.com/google/go-containerregistry/pkg/authn"
	containerregistryname "github.com/google/go-containerregistry/pkg/name"
	containerregistrygoogle "github.com/google/go-containerregistry/pkg/v1/google"
	containerregistryremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/dockerfile/builder"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	"github.com/tsuru/deploy-agent/pkg/build"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

var _ build.Builder = (*BuildKit)(nil)

type BuildKitOptions struct {
	TempDir string
}

type BuildKit struct {
	cli  *client.Client
	opts BuildKitOptions
}

func NewBuildKit(c *client.Client, opts BuildKitOptions) *BuildKit {
	return &BuildKit{cli: c, opts: opts}
}

func (b *BuildKit) Build(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ow, ok := w.(console.File)
	if !ok {
		return nil, errors.New("writer must implement console.File")
	}

	switch pb.BuildKind_name[int32(r.Kind)] {
	case "BUILD_KIND_APP_BUILD_WITH_SOURCE_UPLOAD":
		return b.buildFromAppSourceFiles(ctx, r, ow)

	case "BUILD_KIND_APP_BUILD_WITH_CONTAINER_IMAGE":
		return b.buildFromContainerImage(ctx, r, ow)

	case "BUILD_KIND_PLATFORM_WITH_CONTAINER_FILE":
		return nil, b.buildPlatform(ctx, r, ow)
	}

	return nil, status.Errorf(codes.Unimplemented, "build kind not supported")
}

func (b *BuildKit) buildFromAppSourceFiles(ctx context.Context, r *pb.BuildRequest, w console.File) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	appFiles, err := build.ExtractTsuruAppFilesFromAppSourceContext(ctx, bytes.NewBuffer(r.Data))
	if err != nil {
		return nil, err
	}

	var dockerfile bytes.Buffer
	if err = generateContainerfile(&dockerfile, r.SourceImage, appFiles); err != nil {
		return nil, err
	}

	var envs map[string]string
	if r.App != nil {
		envs = r.App.EnvVars
	}

	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, dockerfile.String(), bytes.NewBuffer(r.Data), envs)
	if err != nil {
		return nil, err
	}
	defer cleanFunc()

	if err = b.callBuildKitBuild(ctx, tmpDir, r, w); err != nil {
		return nil, err
	}

	// NOTE(nettoclaudio): Some platforms doesn't require an user-defined Procfile (e.g. go, java, static, etc).
	// So we need to retrieve the default Procfile from the platform image.
	if appFiles.Procfile == "" {
		fmt.Fprintln(w, "User-defined Procfile not found, trying to extract it from platform's container image")

		tc, err := b.extractTsuruConfigsFromContainerImage(ctx, r.DestinationImages[0])
		if err != nil {
			return nil, err
		}

		appFiles.Procfile = tc.Procfile
	}

	return appFiles, nil
}

func generateContainerfile(w io.Writer, image string, tsuruAppFiles *pb.TsuruConfig) error {
	var tsuruYaml build.TsuruYamlData
	if tsuruAppFiles != nil {
		if err := yaml.Unmarshal([]byte(tsuruAppFiles.TsuruYaml), &tsuruYaml); err != nil {
			return err
		}
	}

	var buildHooks []string
	if hooks := tsuruYaml.Hooks; hooks != nil {
		buildHooks = hooks.Build
	}

	dockerfile, err := build.BuildContainerfile(build.BuildContainerfileParams{
		Image:      image,
		BuildHooks: buildHooks,
	})
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, dockerfile)
	return err
}

func (b *BuildKit) buildFromContainerImage(ctx context.Context, r *pb.BuildRequest, w console.File) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, fmt.Sprintf("FROM %s", r.SourceImage), nil, nil)
	if err != nil {
		return nil, err
	}
	defer cleanFunc()

	if err = b.callBuildKitBuild(ctx, tmpDir, r, w); err != nil {
		return nil, err
	}

	var insecureRegistry bool
	if r.PushOptions != nil {
		insecureRegistry = r.PushOptions.InsecureRegistry
	}

	imageConfig, err := extractContainerImageConfigFromImageManifest(ctx, r.DestinationImages[0], insecureRegistry)
	if err != nil {
		return nil, err
	}

	appFiles, err := b.callBuildKitToExtractTsuruConfigs(ctx, tmpDir)
	if err != nil {
		return nil, err
	}

	appFiles.ImageConfig = imageConfig
	return appFiles, nil
}

func (b *BuildKit) extractTsuruConfigsFromContainerImage(ctx context.Context, image string) (*pb.TsuruConfig, error) {
	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, fmt.Sprintf("FROM %s", image), nil, nil)
	if err != nil {
		return nil, err
	}
	defer cleanFunc()

	return b.callBuildKitToExtractTsuruConfigs(ctx, tmpDir)
}

func (b *BuildKit) callBuildKitToExtractTsuruConfigs(ctx context.Context, localContextDir string) (*pb.TsuruConfig, error) {
	eg, ctx := errgroup.WithContext(ctx)
	pr, pw := io.Pipe() // reader/writer for tar output

	eg.Go(func() error {
		opts := client.SolveOpt{
			LocalDirs: map[string]string{
				"context":    localContextDir,
				"dockerfile": localContextDir,
			},
			Exports: []client.ExportEntry{
				{
					Type: client.ExporterTar,
					Output: func(_ map[string]string) (io.WriteCloser, error) {
						return pw, nil
					},
				},
			},
			Session: []session.Attachable{authprovider.NewDockerAuthProvider(io.Discard)},
		}
		_, err := b.cli.Build(ctx, opts, "deploy-agent", builder.Build, nil)
		return err
	})

	var tc *pb.TsuruConfig
	eg.Go(func() error {
		var err error
		tc, err = build.ExtractTsuruAppFilesFromContainerImageTarball(ctx, pr)
		return err
	})

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return tc, nil
}

func extractContainerImageConfigFromImageManifest(ctx context.Context, imageStr string, insecureRegistry bool) (*pb.ContainerImageConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var nameOpts []containerregistryname.Option
	if insecureRegistry {
		nameOpts = append(nameOpts, containerregistryname.Insecure)
	}

	ref, err := containerregistryname.ParseReference(imageStr, nameOpts...)
	if err != nil {
		return nil, err
	}

	remoteOpts := []containerregistryremote.Option{
		containerregistryremote.WithContext(ctx),
		containerregistryremote.WithAuthFromKeychain(containerregistryauthn.NewMultiKeychain(containerregistryauthn.DefaultKeychain, containerregistrygoogle.Keychain)),
	}

	image, err := containerregistryremote.Image(ref, remoteOpts...)
	if err != nil {
		return nil, err
	}

	cf, err := image.ConfigFile()
	if err != nil {
		return nil, err
	}

	var exposedPorts []string
	for k := range cf.Config.ExposedPorts {
		exposedPorts = append(exposedPorts, k)
	}

	return &pb.ContainerImageConfig{
		Entrypoint:   cf.Config.Entrypoint,
		Cmd:          cf.Config.Cmd,
		WorkingDir:   cf.Config.WorkingDir,
		ExposedPorts: exposedPorts,
	}, nil
}

func generateBuildLocalDir(ctx context.Context, baseDir, dockerfile string, appArchiveData io.Reader, envs map[string]string) (string, func(), error) {
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
		d, nerr := os.Create(filepath.Join(contextRootDir, "Dockerfile"))
		if nerr != nil {
			return status.Errorf(codes.Internal, "cannot create Dockerfile in %s: %s", contextRootDir, nerr)
		}
		defer d.Close()
		_, nerr = io.WriteString(d, dockerfile)
		return nerr
	})

	eg.Go(func() error {
		if appArchiveData == nil { // there's no application.tar.gz file, skipping it
			return nil
		}
		appArchive, nerr := os.Create(filepath.Join(contextRootDir, "application.tar.gz"))
		if nerr != nil {
			return status.Errorf(codes.Internal, "cannot create application archive: %s", nerr)
		}
		defer appArchive.Close()
		_, nerr = io.Copy(appArchive, appArchiveData)
		return nerr
	})

	eg.Go(func() error {
		envsFile, nerr := os.Create(filepath.Join(contextRootDir, "envs.sh"))
		if nerr != nil {
			return nerr
		}
		defer envsFile.Close()
		fmt.Fprintln(envsFile, "# File containing the env vars of Tsuru app. Generated by deploy-agent.")
		for k, v := range envs {
			fmt.Fprintf(envsFile, "%s=%q\n", k, v)
		}
		return nil
	})

	if err = eg.Wait(); err != nil {
		return "", noopFunc, err
	}

	return contextRootDir, func() { os.RemoveAll(contextRootDir) }, nil
}

func (b *BuildKit) buildPlatform(ctx context.Context, r *pb.BuildRequest, w console.File) error {
	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, r.Containerfile, nil, nil)
	if err != nil {
		return err
	}
	defer cleanFunc()

	return b.callBuildKitBuild(ctx, tmpDir, r, w)
}

func (b *BuildKit) callBuildKitBuild(ctx context.Context, buildContextDir string, r *pb.BuildRequest, w console.File) error {
	var secretSources []secretsprovider.Source
	if r.App != nil {
		secretSources = append(secretSources, secretsprovider.Source{
			ID:       "tsuru-app-envvars",
			FilePath: filepath.Join(buildContextDir, "envs.sh"),
		})
	}

	secrets, err := secretsprovider.NewStore(secretSources)
	if err != nil {
		return err
	}

	pw, err := progresswriter.NewPrinter(context.Background(), w, "plain") //nolint - using an empty context intentionally
	if err != nil {
		return err
	}

	eg, nctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		var insecureRegistry bool // disabled by default
		var pushImage bool = true // enabled by default

		if pots := r.PushOptions; pots != nil {
			pushImage = !pots.Disable
			insecureRegistry = pots.InsecureRegistry
		}

		opts := client.SolveOpt{
			LocalDirs: map[string]string{
				"context":    buildContextDir,
				"dockerfile": buildContextDir,
			},
			Exports: []client.ExportEntry{
				{
					Type: client.ExporterImage,
					Attrs: map[string]string{
						"name":              strings.Join(r.DestinationImages, ","),
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
		_, err = b.cli.Build(nctx, opts, "deploy-agent", builder.Build, progresswriter.ResetTime(pw).Status())
		return err
	})

	eg.Go(func() error {
		<-pw.Done()
		return pw.Err()
	})

	return eg.Wait()
}
