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

	"github.com/tsuru/tsuru/exec"
)

type Sidecar struct {
	Client *Client

	// mainContainer is the main container running alongside this sidecar
	mainContainer Container
}

func (s *Sidecar) CommitMainContainer(ctx context.Context, image string) (Image, error) {
	if s.mainContainer.ID == "" {
		if err := s.setup(); err != nil {
			return Image{}, err
		}
	}
	img, err := s.Client.Commit(ctx, s.mainContainer.ID, image)
	if err != nil {
		return Image{}, fmt.Errorf("error commiting image %v: %v", image, err)
	}
	return img, nil
}

// UploadToMainContainer uploads a file to the main container
func (s *Sidecar) UploadToMainContainer(ctx context.Context, fileName string) error {
	if s.mainContainer.ID == "" {
		if err := s.setup(); err != nil {
			return err
		}
	}
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
	return s.Client.Upload(ctx, s.mainContainer.ID, "/", buf)
}

func (s *Sidecar) ExecutorForUser(user string) (exec.Executor, error) {
	if s.mainContainer.ID == "" {
		if err := s.setup(); err != nil {
			return nil, err
		}
	}
	return &Executor{
		Client:      s.Client,
		ContainerID: s.mainContainer.ID,
		DefaultUser: user,
	}, nil
}

func (s *Sidecar) setup() error {
	if s.Client == nil {
		dockerClient, err := NewClient("")
		if err != nil {
			return fmt.Errorf("failed to create docker client: %v", err)
		}
		s.Client = dockerClient
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	mainContainer, err := getMainContainer(ctx, s.Client)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to get main container: %v", err)
	}
	s.mainContainer = mainContainer
	return nil
}

func getMainContainer(ctx context.Context, dockerClient *Client) (Container, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return Container{}, fmt.Errorf("failed to get hostname: %v", err)
	}
	for {
		containers, err := dockerClient.ListContainersByLabels(ctx, map[string]string{
			"io.kubernetes.container.name": hostname,
			"io.kubernetes.pod.name":       hostname,
		})
		if err != nil {
			return Container{}, fmt.Errorf("failed to get containers: %v", err)
		}
		if len(containers) == 1 {
			return containers[0], nil
		}
		select {
		case <-ctx.Done():
			return Container{}, ctx.Err()
		case <-time.After(time.Second * 1):
		}
	}
}
