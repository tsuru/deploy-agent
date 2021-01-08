package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
}

// NewSidecar initializes a Sidecar
func NewSidecar(dockerHost string, user string) (sidecar.Sidecar, error) {
	client, err := newClient(dockerHost)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %v", err)
	}
	sc := dockerSidecar{
		client: client,
	}
	if err = sc.setup(); err != nil {
		return nil, err
	}
	sc.executor = executor{
		client:      sc.client,
		containerID: sc.primaryContainerID,
		defaultUser: user,
	}
	return &sc, nil
}

func (s *dockerSidecar) Executor(ctx context.Context) exec.Executor {
	return &s.executor
}

func (s *dockerSidecar) Commit(ctx context.Context, image string) (string, error) {
	id, err := s.client.commit(ctx, s.primaryContainerID, image)
	if err != nil {
		return "", fmt.Errorf("error commiting image %v: %v", image, err)
	}
	return id, nil
}

// UploadToPrimaryContainer uploads a file to the primary container
func (s *dockerSidecar) Upload(ctx context.Context, fileName string) error {
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open input file %q: %v", fileName, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing file %q: %v", fileName, err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat input file: %v", err)
	}
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: file.Name(),
		Mode: 0666,
		Size: info.Size(),
	}); err != nil {
		return fmt.Errorf("failed to write archive header: %v", err)
	}
	defer func() {
		if err := tw.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing archive: %v", err)
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

func (s *dockerSidecar) BuildImage(ctx context.Context, fileName, image string) error {
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open input file %q: %v", fileName, err)
	}
	defer file.Close()
	return s.client.buildImage(ctx, image, file)
}

func (s *dockerSidecar) TagAndPush(ctx context.Context, baseImage string, destinationImages []string, reg sidecar.RegistryConfig, w io.Writer) error {
	authConfig := docker.AuthConfiguration{
		Username:      reg.RegistryAuthUser,
		Password:      reg.RegistryAuthPass,
		ServerAddress: reg.RegistryAddress,
	}
	for _, destImg := range destinationImages {
		img, err := s.client.tag(ctx, baseImage, destImg)
		if err != nil {
			return fmt.Errorf("error tagging image %v: %v", img, err)
		}
		err = s.pushImage(ctx, img, authConfig, reg.RegistryPushRetries, w)
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
