package containerd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AkihiroSuda/nerdctl/pkg/imgutil/commit"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/errdefs"
	refDocker "github.com/containerd/containerd/reference/docker"
	remoteDocker "github.com/containerd/containerd/remotes/docker"
	"github.com/mholt/archiver/v3"
	"github.com/pkg/errors"
	"github.com/tsuru/deploy-agent/internal/sidecar"
	"github.com/tsuru/tsuru/exec"
)

var _ sidecar.Sidecar = &containerdSidecar{}

var pushHTTPClient = &http.Client{
	Transport: http.DefaultTransport,
	Timeout:   10 * time.Minute,
}

type containerdSidecar struct {
	client             *containerd.Client
	config             SidecarConfig
	primaryContainerID string
	localExec          exec.Executor
}

type SidecarConfig struct {
	Address          string
	User             string
	BuildctlCmd      string
	Standalone       bool
	InsecureRegistry bool
}

func NewSidecar(ctx context.Context, config SidecarConfig) (sidecar.Sidecar, error) {
	client, err := containerd.New(config.Address,
		containerd.WithDefaultNamespace("k8s.io"),
		containerd.WithTimeout(10*time.Minute),
	)
	if err != nil {
		return nil, err
	}
	sc := containerdSidecar{
		client:    client,
		config:    config,
		localExec: exec.OsExecutor{},
	}
	if err = sc.setup(ctx); err != nil {
		return nil, err
	}
	return &sc, nil
}

func (s *containerdSidecar) setup(ctx context.Context) error {
	if s.config.Standalone {
		return nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		return errors.Wrap(err, "failed to get hostname")
	}

	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	filter := fmt.Sprintf("labels.io.kubernetes.container.name==%s,labels.io.kubernetes.pod.name==%s", hostname, hostname)

	for {
		conts, err := s.client.Containers(ctx, filter)
		if err != nil {
			return errors.Wrap(err, "failed to get containers")
		}

		conts, err = s.filterRunningContainers(ctx, conts)
		if err != nil {
			return errors.Wrap(err, "failed to filter containers")
		}

		if len(conts) == 1 {
			s.primaryContainerID = conts[0].ID()
			return nil
		}
		if len(conts) > 1 {
			return errors.Errorf("too many containers matching filters: %d", len(conts))
		}

		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "main container not running")
		case <-time.After(time.Second * 1):
		}
	}
}

func (s *containerdSidecar) filterRunningContainers(ctx context.Context, conts []containerd.Container) ([]containerd.Container, error) {
	var result []containerd.Container
	for _, c := range conts {
		task, err := c.Task(ctx, nil)
		if err != nil {
			continue
		}
		status, err := task.Status(ctx)
		if err != nil {
			continue
		}
		if status.Status == containerd.Running {
			result = append(result, c)
		}
	}
	return result, nil
}

func (s *containerdSidecar) Commit(ctx context.Context, image string) (string, error) {
	imageRef, err := refDocker.ParseDockerRef(image)
	if err != nil {
		return "", err
	}
	digest, err := commit.Commit(ctx, s.client, s.primaryContainerID, &commit.Opts{
		Ref: imageRef.String(),
	})
	if err != nil {
		return "", err
	}
	return digest.String(), nil
}

func (s *containerdSidecar) Upload(ctx context.Context, fileName string) error {
	executor := s.Executor(ctx)
	srcFile, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	info, err := srcFile.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat input file")
	}
	var stdin io.Reader
	if info.Size() > 0 {
		stdin = srcFile
	}
	err = executor.Execute(exec.ExecuteOptions{
		Cmd:   "/bin/sh",
		Args:  []string{"-c", fmt.Sprintf("cat >%s", srcFile.Name())},
		Stdin: stdin,
		Dir:   "/",
	})
	if err != nil {
		return errors.Wrap(err, "failed to execute copy command")
	}
	return nil
}

func (s *containerdSidecar) BuildAndPush(ctx context.Context, fileName string, destinationImages []string, reg sidecar.RegistryConfig, stdout, stderr io.Writer) error {
	// Hardcoded uid as it is directly related what's on the Dockerfile for
	// deploy-agent.
	const uid = 1000

	tmpDir, err := ioutil.TempDir("", "build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tar := archiver.Tar{
		MkdirAll: true,
	}
	err = tar.Unarchive(fileName, tmpDir)
	if err != nil {
		return err
	}
	err = filepath.Walk(tmpDir, func(name string, _ os.FileInfo, err error) error {
		if err == nil {
			err = os.Chown(name, uid, -1)
		}
		return err
	})
	if err != nil {
		return err
	}
	err = s.localExec.Execute(exec.ExecuteOptions{
		Cmd: "sudo",
		Args: []string{
			"-E", "-u", fmt.Sprintf("#%d", uid), "--",
			s.config.BuildctlCmd,
			"build",
			"--frontend", "dockerfile.v0",
			"--local", "context=" + tmpDir,
			"--local", "dockerfile=" + tmpDir,
			"--output", fmt.Sprintf("type=image,\"name=%s\",push=true,registry.insecure=%v", strings.Join(destinationImages, ","), s.config.InsecureRegistry),
		},
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to build image")
	}
	return nil
}

func (s *containerdSidecar) TagAndPush(ctx context.Context, baseImage string, destinationImages []string, reg sidecar.RegistryConfig, w io.Writer) error {
	baseRef, err := refDocker.ParseDockerRef(baseImage)
	if err != nil {
		return err
	}
	image, err := s.client.ImageService().Get(ctx, baseRef.String())
	if err != nil {
		return err
	}

	authorizer := remoteDocker.NewDockerAuthorizer(
		remoteDocker.WithAuthClient(pushHTTPClient),
		remoteDocker.WithAuthCreds(func(string) (string, string, error) {
			return reg.RegistryAuthUser, reg.RegistryAuthPass, nil
		}))
	registryHosts := remoteDocker.ConfigureDefaultRegistries(
		remoteDocker.WithAuthorizer(authorizer),
		remoteDocker.WithClient(pushHTTPClient),
		remoteDocker.WithPlainHTTP(func(host string) (ret bool, err error) {
			local, err := remoteDocker.MatchLocalhost(host)
			if local {
				return local, err
			}
			return s.config.InsecureRegistry, nil
		}),
	)

	tracker := remoteDocker.NewInMemoryTracker()

	resolver := remoteDocker.NewResolver(remoteDocker.ResolverOptions{
		Hosts:   registryHosts,
		Tracker: tracker,
	})

	for _, dstImgName := range destinationImages {
		dstRef, err := refDocker.ParseDockerRef(dstImgName)
		if err != nil {
			return err
		}
		image.Name = dstRef.String()
		_, err = s.client.ImageService().Create(ctx, image)
		if err != nil && !errdefs.IsAlreadyExists(err) {
			return err
		}
		err = pushWithProgress(ctx, s.client, dstRef.String(), image.Target, tracker, resolver, w)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *containerdSidecar) Inspect(ctx context.Context, imageName string) (*sidecar.ImageInspect, error) {
	imageRef, err := refDocker.ParseDockerRef(imageName)
	if err != nil {
		return nil, err
	}
	image, err := s.client.GetImage(ctx, imageRef.String())
	if err != nil {
		return nil, err
	}
	imgConfig, err := readImageConfig(ctx, image)
	if err != nil {
		return nil, err
	}
	if imgConfig.ID == "" {
		imgConfig.ID = image.Name()
	}
	return imgConfig, nil
}

func readImageConfig(ctx context.Context, img containerd.Image) (*sidecar.ImageInspect, error) {
	configDesc, err := img.Config(ctx)
	if err != nil {
		return nil, err
	}

	p, err := content.ReadBlob(ctx, img.ContentStore(), configDesc)
	if err != nil {
		return nil, err
	}

	var config sidecar.ImageInspect
	if err := json.Unmarshal(p, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (s *containerdSidecar) Executor(ctx context.Context) exec.Executor {
	return &containerdExecutor{sidecar: s, ctx: ctx}
}
