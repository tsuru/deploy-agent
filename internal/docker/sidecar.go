package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/tsuru/deploy-agent/internal/sidecar"
	"github.com/tsuru/tsuru/exec"
)

type dockerSidecar struct {
	// executor proxies commands to the primary container
	executor executor

	// primaryContainerID is the container ID running alongside this sidecar
	primaryContainerID string

	// client is a client to the docker daemon
	client *client

	config SidecarConfig
}

type SidecarConfig struct {
	Address    string
	User       string
	Standalone bool
}

// NewSidecar initializes a Sidecar
func NewSidecar(config SidecarConfig) (sidecar.Sidecar, error) {
	client, err := newClient(config.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %v", err)
	}
	sc := dockerSidecar{
		client: client,
		config: config,
	}
	if err = sc.setup(); err != nil {
		return nil, err
	}
	sc.executor = executor{
		client:      sc.client,
		containerID: sc.primaryContainerID,
		defaultUser: config.User,
	}
	return &sc, nil
}

func (s *dockerSidecar) Executor(ctx context.Context) exec.Executor {
	return &s.executor
}

func (s *dockerSidecar) Commit(ctx context.Context, image string) (string, error) {
	id, err := s.client.commit(ctx, s.primaryContainerID, image)
	if err != nil {
		return "", fmt.Errorf("error committing image %v: %v", image, err)
	}
	return id, nil
}

// UploadToPrimaryContainer uploads a file to the primary container
func (s *dockerSidecar) Upload(ctx context.Context, fileName string) (err error) {
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open input file %q: %v", fileName, err)
	}
	defer func() {
		if err = file.Close(); err != nil {
			err = fmt.Errorf("error closing file %q: %v", fileName, err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat input file: %v", err)
	}
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	if err = tw.WriteHeader(&tar.Header{
		Name: file.Name(),
		Mode: 0666,
		Size: info.Size(),
	}); err != nil {
		return fmt.Errorf("failed to write archive header: %v", err)
	}
	defer func() {
		if err = tw.Close(); err != nil {
			err = fmt.Errorf("error closing archive: %v", err)
		}
	}()
	n, err := io.Copy(tw, file)
	if err != nil {
		return fmt.Errorf("failed to write file to archive: %v", err)
	}
	if n != info.Size() {
		return errors.New("short-write copying to archive")
	}
	return s.client.upload(ctx, s.primaryContainerID, "/", buf)
}

func (s *dockerSidecar) BuildAndPush(ctx context.Context, fileName string, destinationImages []string, reg sidecar.RegistryConfig, stdout, stderr io.Writer) error {
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open input file %q: %v", fileName, err)
	}
	defer file.Close()
	err = s.client.buildImage(ctx, destinationImages[0], file, stdout)
	if err != nil {
		return err
	}
	return s.TagAndPush(ctx, destinationImages[0], destinationImages, reg, stdout)
}

func (s *dockerSidecar) TagAndPush(ctx context.Context, baseImage string, destinationImages []string, reg sidecar.RegistryConfig, w io.Writer) error {
	baseAuthConfig := &docker.AuthConfiguration{
		Username:      reg.RegistryAuthUser,
		Password:      reg.RegistryAuthPass,
		ServerAddress: reg.RegistryAddress,
	}
	for _, destImg := range destinationImages {
		registry, _, _ := splitImageName(destImg)
		authConfig := loadCreds(registry, w)
		if authConfig == nil {
			authConfig = baseAuthConfig
		}
		img, err := s.client.tag(ctx, baseImage, destImg)
		if err != nil {
			return fmt.Errorf("error tagging image %v: %v", img, err)
		}
		err = s.pushImage(ctx, img, *authConfig, reg.RegistryPushRetries, w)
		if err != nil {
			return fmt.Errorf("error pushing image %v: %v", img, err)
		}
	}
	return nil
}

func (s *dockerSidecar) Inspect(ctx context.Context, image string) (*sidecar.ImageInspect, error) {
	imgInspect, err := s.client.inspect(context.Background(), image)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image %q: %v", image, err)
	}
	return (*sidecar.ImageInspect)(imgInspect), nil
}

func (s *dockerSidecar) pushImage(ctx context.Context, img image, auth docker.AuthConfiguration, retries int, w io.Writer) error {
	fmt.Fprintf(w, " ---> Sending image to repository (%s)\n", img)
	var err error
	for i := 0; i < retries; i++ {
		err = s.client.push(ctx, auth, img)
		if err != nil {
			fmt.Fprintf(w, "Could not send image, trying again. Original error: %v\n", err)
			time.Sleep(time.Second)
			continue
		}
		break
	}
	if err != nil {
		return fmt.Errorf("Error pushing image: %v", err)
	}
	return nil
}

func (s *dockerSidecar) setup() error {
	if s.config.Standalone {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	id, err := getPrimaryContainerID(ctx, s.client)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to get main container: %v", err)
	}
	s.primaryContainerID = id
	return nil
}

func getPrimaryContainerID(ctx context.Context, dockerClient *client) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %v", err)
	}
	for {
		containers, err := dockerClient.listContainersByLabels(ctx, map[string]string{
			"io.kubernetes.container.name": hostname,
			"io.kubernetes.pod.name":       hostname,
		})
		if err != nil {
			return "", fmt.Errorf("failed to get containers: %v", err)
		}
		if len(containers) == 1 {
			return containers[0].ID, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second * 1):
		}
	}
}

func loadCreds(registry string, w io.Writer) *docker.AuthConfiguration {
	authConfig, err := docker.NewAuthConfigurationsFromCredsHelpers(registry)
	if err == nil {
		return authConfig
	}
	os.Setenv("DOCKER_CONFIG", path.Join(os.Getenv("HOME"), "original-docker-config"))
	defer os.Unsetenv("DOCKER_CONFIG")
	authConfig, err = docker.NewAuthConfigurationsFromCredsHelpers(registry)
	if err == nil {
		return authConfig
	}
	return nil
}
