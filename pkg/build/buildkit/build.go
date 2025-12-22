// Copyright 2025 tsuru authors. All rights reserved.
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
	"sync"
	"time"

	"github.com/alessio/shellescape"
	"github.com/containerd/console"
	"github.com/docker/cli/cli/config"
	containerregistryauthn "github.com/google/go-containerregistry/pkg/authn"
	containerregistryname "github.com/google/go-containerregistry/pkg/name"
	containerregistrygoogle "github.com/google/go-containerregistry/pkg/v1/google"
	containerregistryremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/tsuru/deploy-agent/pkg/build"
	"github.com/tsuru/deploy-agent/pkg/build/buildkit/autodiscovery"
	"github.com/tsuru/deploy-agent/pkg/build/buildkit/metrics"
	"github.com/tsuru/deploy-agent/pkg/build/buildkit/scaler"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
	repo "github.com/tsuru/deploy-agent/pkg/repository"
	"github.com/tsuru/deploy-agent/pkg/util"
)

const defaultBuildKitNamespace = "tsuru-system"

var _ build.Builder = (*BuildKit)(nil)

type BuildKitOptions struct {
	RemoteRepository             map[string]repo.Repository
	TempDir                      string
	DiscoverBuildKitClientForApp bool
	DisableCache                 bool
}

type BuildKit struct {
	cli    *client.Client
	k8s    *kubernetes.Clientset
	dk8s   dynamic.Interface
	kdopts *autodiscovery.KubernertesDiscoveryOptions
	opts   BuildKitOptions
	m      sync.RWMutex
}

func NewBuildKit(c *client.Client, opts BuildKitOptions) *BuildKit {
	return &BuildKit{cli: c, opts: opts}
}

func (b *BuildKit) WithKubernetesDiscovery(cs *kubernetes.Clientset, dcs dynamic.Interface, opts autodiscovery.KubernertesDiscoveryOptions) *BuildKit {
	b.k8s = cs
	b.dk8s = dcs
	b.kdopts = &opts

	if opts.Statefulset != "" {
		scaler.StartWorker(cs, opts.PodSelector, opts.Statefulset, opts.ScaleGracefulPeriod)
	}

	return b
}

func (b *BuildKit) Close() error {
	b.m.Lock()
	defer b.m.Unlock()

	if b.cli == nil {
		return nil
	}

	return b.cli.Close()
}

func (b *BuildKit) Build(ctx context.Context, r *pb.BuildRequest, w io.Writer) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ow, ok := w.(console.File)
	if !ok {
		return nil, errors.New("writer must implement console.File")
	}

	c, clientCleanUp, buildkitNamespace, err := b.client(ctx, r, w)
	if err != nil {
		return nil, err
	}

	if clientCleanUp != nil {
		defer clientCleanUp()
	}

	startTime := time.Now()
	metrics.BuildsActive.WithLabelValues(buildkitNamespace).Inc()
	defer func() {
		metrics.BuildDuration.WithLabelValues(buildkitNamespace).Observe(time.Since(startTime).Seconds())
		metrics.BuildsActive.WithLabelValues(buildkitNamespace).Dec()
	}()
	buildKind := pb.BuildKind_name[int32(r.Kind)]
	metrics.BuildsTotal.WithLabelValues(buildkitNamespace, buildKind).Inc()
	switch buildKind {
	case "BUILD_KIND_APP_BUILD_WITH_SOURCE_UPLOAD":
		return b.buildFromAppSourceFiles(ctx, c, r, ow)

	case "BUILD_KIND_APP_BUILD_WITH_CONTAINER_IMAGE":
		return b.buildFromContainerImage(ctx, c, r, ow)

	case "BUILD_KIND_JOB_CREATE_WITH_CONTAINER_IMAGE":
		return b.buildFromContainerImage(ctx, c, r, ow)

	case "BUILD_KIND_APP_BUILD_WITH_CONTAINER_FILE":
		return b.buildFromContainerFile(ctx, c, r, ow)

	case "BUILD_KIND_JOB_DEPLOY_WITH_CONTAINER_FILE":
		return b.buildFromContainerFile(ctx, c, r, ow)

	case "BUILD_KIND_PLATFORM_WITH_CONTAINER_FILE":
		return nil, b.buildPlatform(ctx, c, r, ow)
	default:
		return nil, status.Errorf(codes.Unimplemented, "build kind not supported")
	}
}

func (b *BuildKit) buildFromAppSourceFiles(ctx context.Context, c *client.Client, r *pb.BuildRequest, w console.File) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	appFiles, err := build.ExtractTsuruAppFilesFromAppSourceContext(ctx, bytes.NewBuffer(r.Data))
	if err != nil {
		return nil, err
	}

	var dockerfile bytes.Buffer
	tsuruYAML, err := parseTsuruYaml(appFiles.TsuruYaml)
	if err != nil {
		return nil, err
	}

	if err = generateContainerfile(&dockerfile, r.SourceImage, tsuruYAML.Hooks); err != nil {
		return nil, err
	}

	var envs map[string]string
	if r.App != nil {
		envs = r.App.EnvVars
	}

	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, dockerfile.String(), bytes.NewBuffer(r.Data), envs, nil)
	if err != nil {
		return nil, err
	}
	defer cleanFunc()

	if b.opts.RemoteRepository != nil {
		err = b.createRemoteRepository(ctx, r)
		if err != nil {
			return nil, err
		}
	}

	if err = callBuildKitBuild(ctx, c, tmpDir, r, w, b.opts.DisableCache); err != nil {
		return nil, err
	}

	// NOTE(nettoclaudio): Some platforms don't require an user-defined Procfile (e.g. go, java, static, etc).
	// So we need to retrieve the default Procfile from the platform image.
	if appFiles.Procfile == "" && len(tsuruYAML.Processes) == 0 {
		fmt.Fprintln(w, "User-defined Procfile/Tsuru YAML Processes not found, trying to extract it from platform's container image")

		tc, err := b.extractTsuruConfigsFromContainerImage(ctx, c, r.DestinationImages[0], build.DefaultTsuruPlatformWorkingDir)
		if err != nil {
			return nil, err
		}

		appFiles.Procfile = tc.Procfile
	}

	return appFiles, nil
}

func parseTsuruYaml(tsuruYAMLContent string) (build.TsuruYamlData, error) {
	var tsuruYAML build.TsuruYamlData
	if strings.TrimSpace(tsuruYAMLContent) == "" {
		return build.TsuruYamlData{}, nil
	}
	if err := yaml.Unmarshal([]byte(tsuruYAMLContent), &tsuruYAML); err != nil {
		return build.TsuruYamlData{}, err
	}
	return tsuruYAML, nil
}

func generateContainerfile(w io.Writer, image string, tsuruYamlHooks *build.TsuruYamlHooks) error {
	var buildHooks []string
	if tsuruYamlHooks != nil {
		buildHooks = tsuruYamlHooks.Build
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

func findAndReadTsuruYaml(tmpDir string) (string, error) {
	contextDir := filepath.Join(tmpDir, "context")

	// Try all possible Tsuru YAML filenames
	for _, filename := range build.TsuruYamlNames {
		tsuruYamlPath := filepath.Join(contextDir, filename)
		if _, err := os.Stat(tsuruYamlPath); err == nil {
			tsuruYamlData, err := os.ReadFile(tsuruYamlPath)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", filename, err)
			}
			return string(tsuruYamlData), nil
		}
	}

	// No Tsuru YAML file found, return empty string (not an error)
	return "", nil
}

func (b *BuildKit) buildFromContainerImage(ctx context.Context, c *client.Client, r *pb.BuildRequest, w console.File) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, fmt.Sprintf("FROM %s", r.SourceImage), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer cleanFunc()

	if b.opts.RemoteRepository != nil {
		err = b.createRemoteRepository(ctx, r)
		if err != nil {
			return nil, err
		}
	}

	if err = callBuildKitBuild(ctx, c, tmpDir, r, w, b.opts.DisableCache); err != nil {
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

	appFiles, err := callBuildKitToExtractTsuruConfigs(ctx, c, tmpDir, imageConfig.WorkingDir)
	if err != nil {
		return nil, err
	}

	appFiles.ImageConfig = imageConfig
	return appFiles, nil
}

func (b *BuildKit) extractTsuruConfigsFromContainerImage(ctx context.Context, c *client.Client, image, workingDir string) (*pb.TsuruConfig, error) {
	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, fmt.Sprintf("FROM %s", image), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer cleanFunc()

	return callBuildKitToExtractTsuruConfigs(ctx, c, tmpDir, workingDir)
}

func (b *BuildKit) createRemoteRepository(ctx context.Context, r *pb.BuildRequest) error {
	for _, v := range r.DestinationImages {
		if provider, ok := b.opts.RemoteRepository[build.GetRegistry(v)]; ok {
			err := provider.Ensure(ctx, v)
			if err != nil {
				return err
			}
		}
	}
	return nil
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

	return &pb.ContainerImageConfig{
		Entrypoint:   cf.Config.Entrypoint,
		Cmd:          cf.Config.Cmd,
		WorkingDir:   cf.Config.WorkingDir,
		ExposedPorts: build.SortExposedPorts(cf.Config.ExposedPorts),
	}, nil
}

func generateBuildLocalDir(ctx context.Context, baseDir, dockerfile string, appArchiveData io.Reader, envs map[string]string, files io.Reader) (string, func(), error) {
	noopFunc := func() {}

	if err := ctx.Err(); err != nil {
		return "", noopFunc, err
	}

	// Layout design
	//
	// ./                       # Root dir
	//   Dockerfile
	//   secrets/
	//     envs.sh              # Tsuru app's env vars
	//   context/
	//     application.tar.gz   # Tsuru app's deploy data
	//     ...
	//     [other files]

	rootDir, err := os.MkdirTemp(baseDir, "deploy-agent-*")
	if err != nil {
		return "", noopFunc, status.Errorf(codes.Internal, "failed to create temp dir: %s", err)
	}

	contextDir := filepath.Join(rootDir, "context")
	if err = os.Mkdir(contextDir, 0o755); err != nil {
		return "", noopFunc, err
	}

	secretsDir := filepath.Join(rootDir, "secrets")
	if err = os.Mkdir(secretsDir, 0o700); err != nil {
		return "", noopFunc, err
	}

	eg, nctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		d, nerr := os.Create(filepath.Join(rootDir, "Dockerfile"))
		if nerr != nil {
			return status.Errorf(codes.Internal, "cannot create Dockerfile in %s: %s", rootDir, nerr)
		}
		defer d.Close()
		_, nerr = io.WriteString(d, dockerfile)
		return nerr
	})

	eg.Go(func() error {
		if appArchiveData == nil { // there's no application.tar.gz file, skipping it
			return nil
		}
		appArchive, nerr := os.Create(filepath.Join(contextDir, "application.tar.gz"))
		if nerr != nil {
			return status.Errorf(codes.Internal, "cannot create application archive: %s", nerr)
		}
		defer appArchive.Close()
		_, nerr = io.Copy(appArchive, appArchiveData)
		return nerr
	})

	eg.Go(func() error {
		envsFile, nerr := os.Create(filepath.Join(secretsDir, "envs.sh"))
		if nerr != nil {
			return nerr
		}
		defer envsFile.Close()
		fmt.Fprintln(envsFile, "# File containing the env vars of Tsuru app. Generated by deploy-agent.")
		for k, v := range envs {
			fmt.Fprintf(envsFile, "export %s=%s\n", k, shellescape.Quote(v))
		}
		return nil
	})

	eg.Go(func() error {
		if files == nil {
			return nil
		}

		return util.ExtractGZIPFileToDir(nctx, files, contextDir)
	})

	if err = eg.Wait(); err != nil {
		return "", noopFunc, err
	}

	return rootDir, func() { os.RemoveAll(rootDir) }, nil
}

func (b *BuildKit) buildFromContainerFile(ctx context.Context, c *client.Client, r *pb.BuildRequest, w console.File) (*pb.TsuruConfig, error) {
	var files io.Reader
	if len(r.Data) > 0 {
		files = bytes.NewReader(r.Data)
	}

	envVars := map[string]string{}
	if r.App != nil {
		envVars = r.App.EnvVars
	} else if r.Job != nil {
		envVars = r.Job.EnvVars
	}

	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, r.Containerfile, nil, envVars, files)
	if err != nil {
		return nil, err
	}
	defer cleanFunc()

	if b.opts.RemoteRepository != nil {
		err = b.createRemoteRepository(ctx, r)
		if err != nil {
			return nil, err
		}
	}

	if err = callBuildKitBuild(ctx, c, tmpDir, r, w, b.opts.DisableCache); err != nil {
		return nil, err
	}

	var insecureRegistry bool
	if r.PushOptions != nil {
		insecureRegistry = r.PushOptions.InsecureRegistry
	}

	ic, err := extractContainerImageConfigFromImageManifest(ctx, r.DestinationImages[0], insecureRegistry)
	if err != nil {
		return nil, err
	}

	tc, err := b.extractTsuruConfigsFromContainerImage(ctx, c, r.DestinationImages[0], ic.WorkingDir)
	if err != nil {
		return nil, err
	}

	if tc.TsuruYaml == "" {
		tsuruYamlData, err := findAndReadTsuruYaml(tmpDir)
		if err != nil {
			return nil, err
		}
		if tsuruYamlData != "" {
			fmt.Fprintln(w, "Found user-defined tsuru.yaml file in build context, using it.")
			tc.TsuruYaml = tsuruYamlData
		}
	}

	tc.ImageConfig = ic

	return tc, nil
}

func (b *BuildKit) buildPlatform(ctx context.Context, c *client.Client, r *pb.BuildRequest, w console.File) error {
	tmpDir, cleanFunc, err := generateBuildLocalDir(ctx, b.opts.TempDir, r.Containerfile, nil, nil, nil)
	if err != nil {
		return err
	}
	defer cleanFunc()
	if b.opts.RemoteRepository != nil {
		err = b.createRemoteRepository(ctx, r)
		if err != nil {
			return err
		}
	}
	return callBuildKitBuild(ctx, c, tmpDir, r, w, b.opts.DisableCache)
}

func callBuildKitBuild(ctx context.Context, c *client.Client, buildContextDir string, r *pb.BuildRequest, w console.File, disableCache bool) error {
	// Force prune when cache is disabled
	if disableCache {
		fmt.Fprintln(w, "Cache disabled, performing remote prune before build...")
		if err := c.Prune(ctx, nil, client.PruneAll); err != nil {
			fmt.Fprintf(w, "Warning: Failed to prune remote cache: %v\n", err)
		} else {
			fmt.Fprintln(w, "Remote cache pruned successfully")
		}
	}

	var secretSources []secretsprovider.Source
	if r.App != nil {
		secretSources = append(secretSources, secretsprovider.Source{
			ID:       "tsuru-app-envvars",
			FilePath: filepath.Join(buildContextDir, "secrets", "envs.sh"),
		})
	} else if r.Job != nil {
		secretSources = append(secretSources, secretsprovider.Source{
			ID:       "tsuru-job-envvars",
			FilePath: filepath.Join(buildContextDir, "secrets", "envs.sh"),
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

		frontendAttrs := map[string]string{
			// NOTE: we should always run the deploy's script command as user might
			// need to regenerate assets, for example.
			"build-arg:tsuru_deploy_cache": strconv.FormatInt(time.Now().Unix(), 10),
		}

		if disableCache {
			frontendAttrs["no-cache"] = ""
		}

		opts := client.SolveOpt{
			Frontend:      "dockerfile.v0",
			FrontendAttrs: frontendAttrs,
			LocalDirs: map[string]string{
				"context":    filepath.Join(buildContextDir, "context"),
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
				authprovider.NewDockerAuthProvider(config.LoadDefaultConfigFile(os.Stderr)),
				secretsprovider.NewSecretProvider(secrets),
			},
		}

		_, err = c.Build(nctx, opts, "deploy-agent", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			return c.Solve(ctx, gateway.SolveRequest{
				Frontend:    opts.Frontend,
				FrontendOpt: opts.FrontendAttrs,
			})
		}, progresswriter.ResetTime(pw).Status())
		return err
	})

	eg.Go(func() error {
		<-pw.Done()
		return pw.Err()
	})

	return eg.Wait()
}

func callBuildKitToExtractTsuruConfigs(ctx context.Context, c *client.Client, localContextDir, workingDir string) (*pb.TsuruConfig, error) {
	eg, ctx := errgroup.WithContext(ctx)
	pr, pw := io.Pipe() // reader/writer for tar output

	eg.Go(func() error {
		opts := client.SolveOpt{
			Frontend: "dockerfile.v0",
			LocalDirs: map[string]string{
				"context":    filepath.Join(localContextDir, "context"),
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
			Session: []session.Attachable{
				authprovider.NewDockerAuthProvider(config.LoadDefaultConfigFile(os.Stderr)),
			},
		}
		_, err := c.Build(ctx, opts, "deploy-agent", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			return c.Solve(ctx, gateway.SolveRequest{
				Frontend:    opts.Frontend,
				FrontendOpt: opts.FrontendAttrs,
			})
		}, nil)
		return err
	})

	var tc *pb.TsuruConfig
	eg.Go(func() error {
		var err error
		tc, err = build.ExtractTsuruAppFilesFromContainerImageTarball(ctx, pr, workingDir)
		return err
	})

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return tc, nil
}

type clientCleanUp func()

func (b *BuildKit) client(ctx context.Context, req *pb.BuildRequest, w io.Writer) (*client.Client, clientCleanUp, string, error) {
	isBuildForApp := strings.HasPrefix(pb.BuildKind_name[int32(req.Kind)], "BUILD_KIND_APP_")

	if isBuildForApp && b.opts.DiscoverBuildKitClientForApp {
		d := &autodiscovery.K8sDiscoverer{
			KubernetesInterface: b.k8s,
			DynamicInterface:    b.dk8s,
		}
		return d.Discover(ctx, *b.kdopts, req, w)
	}

	return b.cli, func() {}, defaultBuildKitNamespace, nil
}
