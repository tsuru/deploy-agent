package sidecar

import (
	"context"
	"io"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/tsuru/tsuru/exec"
)

type ImageInspect docker.Image

type RegistryConfig struct {
	RegistryAuthUser    string
	RegistryAuthPass    string
	RegistryAddress     string
	RegistryPushRetries int
}

type Filesystem interface {
	ReadFile(name string) ([]byte, error)
	CheckFile(name string) (bool, error)
	RemoveFile(name string) error
}

type Sidecar interface {
	Commit(ctx context.Context, image string) (string, error)
	Upload(ctx context.Context, fileName string) error
	BuildImage(ctx context.Context, fileName, image string) error
	TagAndPush(ctx context.Context, baseImage string, destinationImages []string, reg RegistryConfig, w io.Writer) error
	Inspect(ctx context.Context, image string) (*ImageInspect, error)
	Executor() exec.Executor
}
