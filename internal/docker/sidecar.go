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
)

type Sidecar struct {
	// Executor proxies commands to the primary container
	Executor

	client *Client

	// primaryContainer is the container running alongside this sidecar
	primaryContainer Container
}

// NewSidecar initializes a Sidecar
func NewSidecar(client *Client, user string) (*Sidecar, error) {
	if client == nil {
		var err error
		client, err = NewClient("")
		if err != nil {
			return nil, fmt.Errorf("failed to create docker client: %v", err)
		}
	}
	sidecar := Sidecar{client: client}
	if err := sidecar.setup(); err != nil {
		return nil, err
	}
	sidecar.Executor = Executor{
		Client:      sidecar.client,
		ContainerID: sidecar.primaryContainer.ID,
		DefaultUser: user,
	}
	return &sidecar, nil
}

func (s *Sidecar) CommitPrimaryContainer(ctx context.Context, image string) (string, error) {
	id, err := s.client.Commit(ctx, s.primaryContainer.ID, image)
	if err != nil {
		return "", fmt.Errorf("error commiting image %v: %v", image, err)
	}
	return id, nil
}

// UploadToPrimaryContainer uploads a file to the primary container
func (s *Sidecar) UploadToPrimaryContainer(ctx context.Context, fileName string) error {
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
	return s.client.Upload(ctx, s.primaryContainer.ID, "/", buf)
}

func (s *Sidecar) setup() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	mainContainer, err := getPrimaryContainer(ctx, s.client)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to get main container: %v", err)
	}
	s.primaryContainer = mainContainer
	return nil
}

func getPrimaryContainer(ctx context.Context, dockerClient *Client) (Container, error) {
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
