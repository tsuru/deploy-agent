// Copyright 2023 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildkit_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	dockertypescontainer "github.com/docker/docker/api/types/container"
	dockerstrslice "github.com/docker/docker/api/types/strslice"
	dockerclient "github.com/docker/docker/client"
	dockerstdcopy "github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/tsuru/deploy-agent/pkg/build/buildkit"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

var (
	buildkitHost string
	dockerHost   string

	registryAddress   string
	registryNamespace string
	registryHTTP      bool
)

func TestMain(m *testing.M) {
	found, _ := strconv.ParseBool(os.Getenv("DEPLOY_AGENT_INTEGRATION"))
	if !found {
		fmt.Println("Skipping deploy agent v2 integration tests")
		return
	}

	buildkitHost, found = os.LookupEnv("BUILDKIT_HOST")
	if !found {
		fmt.Println("Skipping deploy agent v2 integration tests: missing BUILDKIT_HOST env var")
		return
	}

	dockerHost, found = os.LookupEnv("DOCKER_HOST")
	if !found {
		fmt.Println("Skipping deploy agent v2 integration tests: missing DOCKER_HOST env var")
		return
	}

	registryAddress, found = os.LookupEnv("DEPLOY_AGENT_INTEGRATION_REGISTRY_HOST")
	if !found {
		fmt.Println("Skipping deploy agent v2 integration tests: missing DEPLOY_AGENT_INTEGRATION_REGISTRY_HOST envs")
		return
	}

	registryNamespace, found = os.LookupEnv("DEPLOY_AGENT_INTEGRATION_REGISTRY_NAMESPACE")
	if !found {
		registryNamespace = "deploy-agent-integration"
	}

	registryHTTP, _ = strconv.ParseBool(os.Getenv("DEPLOY_AGENT_INTEGRATION_REGISTRY_HTTP"))

	os.Exit(m.Run())
}

func TestBuildKit_Build_FromSourceFiles(t *testing.T) {
	destImage := baseRegistry(t, "python", "latest")

	req := &pb.BuildRequest{
		Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_SOURCE_UPLOAD,
		App: &pb.TsuruApp{
			Name: "my-app",
			EnvVars: map[string]string{
				"MY_ENV_VAR":        "my awesome env var :P",
				"PYTHON_VERSION":    "3.10.4",
				"DATABASE_PASSWORD": "a@3a`fo@&$(ls -lah)",
			},
		},
		SourceImage:       "tsuru/python:latest",
		DestinationImages: []string{destImage},
		Data:              appArchiveData(t, "./testdata/python/"),
		PushOptions:       &pb.PushOptions{InsecureRegistry: registryHTTP},
	}

	bc := newBuildKitClient(t)
	defer bc.Close()

	appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).
		Build(context.TODO(), req, os.Stdout)

	assert.NoError(t, err)
	assert.Equal(t, &pb.TsuruConfig{
		Procfile:  "web: python app.py\n",
		TsuruYaml: "hooks:\n  build:\n  - touch /tmp/foo\n  - |-\n    mkdir -p /tmp/tsuru \\\n    && echo \"MY_ENV_VAR=${MY_ENV_VAR}\" > /tmp/tsuru/envs \\\n    && echo \"DATABASE_PASSWORD=${DATABASE_PASSWORD}\" >> /tmp/tsuru/envs\n  - python --version\n\nhealthcheck:\n  path: /\n",
	}, appFiles)

	dc := newDockerClient(t)
	defer dc.Close()

	r, err := dc.ImagePull(context.TODO(), destImage, dockertypes.ImagePullOptions{})
	require.NoError(t, err)
	defer r.Close()

	fmt.Println("Pulling container image", destImage)
	_, err = io.Copy(os.Stdout, r)
	require.NoError(t, err)

	defer func() {
		fmt.Printf("Removing container image %s\n", destImage)
		_, nerr := dc.ImageRemove(context.TODO(), destImage, dockertypes.ImageRemoveOptions{Force: true})
		require.NoError(t, nerr)
	}()

	containerCreateResp, err := dc.ContainerCreate(context.TODO(), &dockertypescontainer.Config{
		Image: destImage,
		Cmd:   dockerstrslice.StrSlice{"sleep", "Inf"},
	}, nil, nil, nil, "")
	require.NoError(t, err)
	require.NotEmpty(t, containerCreateResp.ID, "container ID cannot be empty")

	containerID := containerCreateResp.ID
	fmt.Printf("Container created (ID=%s)\n", containerID)

	defer func() {
		fmt.Printf("Removing container (ID=%s)\n", containerID)
		require.NoError(t, dc.ContainerRemove(context.TODO(), containerID, dockertypes.ContainerRemoveOptions{Force: true, RemoveVolumes: true}))
	}()

	err = dc.ContainerStart(context.TODO(), containerID, dockertypes.ContainerStartOptions{})
	require.NoError(t, err)
	fmt.Printf("Starting container (ID=%s)\n", containerID)

	t.Run("ensure app archive is on expected location", func(t *testing.T) {
		execCreateResp, err := dc.ContainerExecCreate(context.TODO(), containerID, dockertypes.ExecConfig{
			Tty:          true,
			AttachStderr: true,
			AttachStdout: true,
			Cmd:          []string{"sha256sum", "/home/application/archive.tar.gz"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, execCreateResp.ID, "exec ID cannot be empty")

		execID := execCreateResp.ID

		hijackedResp, err := dc.ContainerExecAttach(context.TODO(), execID, dockertypes.ExecStartCheck{})
		require.NoError(t, err)
		require.NotNil(t, hijackedResp.Reader)
		defer hijackedResp.Close()

		var stderr, stdout bytes.Buffer
		_, err = dockerstdcopy.StdCopy(&stdout, &stderr, hijackedResp.Reader)
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("%x  /home/application/archive.tar.gz\r\n", sha256.Sum256(req.Data)), stdout.String())
		assert.Empty(t, stderr.String())

		execInspectResp, err := dc.ContainerExecInspect(context.TODO(), execID)
		require.NoError(t, err)
		assert.Equal(t, execID, execInspectResp.ExecID)
		assert.Equal(t, containerID, execInspectResp.ContainerID)
		assert.False(t, execInspectResp.Running)
		assert.Empty(t, execInspectResp.ExitCode)
		assert.NotEmpty(t, execInspectResp.Pid)
	})

	t.Run("ensure build hooks were executed", func(t *testing.T) {
		execCreateResp, err := dc.ContainerExecCreate(context.TODO(), containerID, dockertypes.ExecConfig{
			Tty:          true,
			AttachStderr: true,
			AttachStdout: true,
			Cmd:          []string{"cat", "/tmp/tsuru/envs"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, execCreateResp.ID, "exec ID cannot be empty")

		execID := execCreateResp.ID

		hijackedResp, err := dc.ContainerExecAttach(context.TODO(), execID, dockertypes.ExecStartCheck{})
		require.NoError(t, err)
		require.NotNil(t, hijackedResp.Reader)
		defer hijackedResp.Close()

		var stderr, stdout bytes.Buffer
		_, err = dockerstdcopy.StdCopy(&stdout, &stderr, hijackedResp.Reader)
		require.NoError(t, err)
		assert.Equal(t, "MY_ENV_VAR=my awesome env var :P\r\nDATABASE_PASSWORD=a@3a`fo@&$(ls -lah)\r\n", stdout.String())
		assert.Empty(t, stderr.String())

		execInspectResp, err := dc.ContainerExecInspect(context.TODO(), execID)
		require.NoError(t, err)
		assert.Equal(t, execID, execInspectResp.ExecID)
		assert.Equal(t, containerID, execInspectResp.ContainerID)
		assert.False(t, execInspectResp.Running)
		assert.Empty(t, execInspectResp.ExitCode)
		assert.NotEmpty(t, execInspectResp.Pid)
	})

	t.Run("check if system packages were installed", func(t *testing.T) {
		execCreateResp, err := dc.ContainerExecCreate(context.TODO(), containerID, dockertypes.ExecConfig{
			Tty:          true,
			AttachStderr: true,
			AttachStdout: true,
			Cmd:          []string{"tcpdump", "--version"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, execCreateResp.ID, "exec ID cannot be empty")

		execID := execCreateResp.ID

		hijackedResp, err := dc.ContainerExecAttach(context.TODO(), execID, dockertypes.ExecStartCheck{})
		require.NoError(t, err)
		require.NotNil(t, hijackedResp.Reader)
		defer hijackedResp.Close()

		var stderr, stdout bytes.Buffer
		_, err = dockerstdcopy.StdCopy(&stdout, &stderr, hijackedResp.Reader)
		require.NoError(t, err)
		assert.Regexp(t, `(.*)tcpdump version (.*)`, stdout.String())
		assert.Empty(t, stderr.String())

		execInspectResp, err := dc.ContainerExecInspect(context.TODO(), execID)
		require.NoError(t, err)
		assert.Equal(t, execID, execInspectResp.ExecID)
		assert.Equal(t, containerID, execInspectResp.ContainerID)
		assert.False(t, execInspectResp.Running)
		assert.Empty(t, execInspectResp.ExitCode)
		assert.NotEmpty(t, execInspectResp.Pid)
	})

	t.Run("ensure the specific python version was installed", func(t *testing.T) {
		execCreateResp, err := dc.ContainerExecCreate(context.TODO(), containerID, dockertypes.ExecConfig{
			Tty:          true,
			AttachStderr: true,
			AttachStdout: true,
			Cmd:          []string{"bash", "-lc", "python --version"}, // bash -l is mandatory to force loading the ~/.profile file which includes python in the PATH
		})
		require.NoError(t, err)
		require.NotEmpty(t, execCreateResp.ID, "exec ID cannot be empty")

		execID := execCreateResp.ID

		hijackedResp, err := dc.ContainerExecAttach(context.TODO(), execID, dockertypes.ExecStartCheck{})
		require.NoError(t, err)
		require.NotNil(t, hijackedResp.Reader)
		defer hijackedResp.Close()

		var stderr, stdout bytes.Buffer
		_, err = dockerstdcopy.StdCopy(&stdout, &stderr, hijackedResp.Reader)
		require.NoError(t, err)
		assert.Regexp(t, `Python 3.10.4\s`, stdout.String())
		assert.Empty(t, stderr.String())

		execInspectResp, err := dc.ContainerExecInspect(context.TODO(), execID)
		require.NoError(t, err)
		assert.Equal(t, execID, execInspectResp.ExecID)
		assert.Equal(t, containerID, execInspectResp.ContainerID)
		assert.False(t, execInspectResp.Running)
		assert.Empty(t, execInspectResp.ExitCode)
		assert.NotEmpty(t, execInspectResp.Pid)
	})
}

func TestBuildKit_Build_AppDeployFromSourceFiles_NoUserDefinedProcfile(t *testing.T) {
	destImage := baseRegistry(t, "my-static-app", "latest")

	req := &pb.BuildRequest{
		Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_SOURCE_UPLOAD,
		App: &pb.TsuruApp{
			Name: "my-app",
		},
		SourceImage:       "tsuru/static:2.3",
		DestinationImages: []string{destImage},
		Data:              appArchiveData(t, "./testdata/static/"),
		PushOptions:       &pb.PushOptions{InsecureRegistry: registryHTTP},
	}

	bc := newBuildKitClient(t)
	defer bc.Close()

	appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).
		Build(context.TODO(), req, os.Stdout)

	require.NoError(t, err)
	assert.Equal(t, &pb.TsuruConfig{
		Procfile: "web: /usr/sbin/nginx -g \"daemon off;\"\n",
	}, appFiles)
}

func TestBuildKit_Build_FromContainerImages(t *testing.T) {
	dc := newDockerClient(t)
	defer dc.Close()

	bc := newBuildKitClient(t)
	defer bc.Close()

	t.Run("container image that contains Tsuru app files (tsuru.yaml, Procfile)", func(t *testing.T) {
		srcImage := baseRegistry(t, "my-container", "latest")

		data := appArchiveData(t, "./testdata/container_image/")
		r := bytes.NewReader(data)

		buildResp, err := dc.ImageBuild(context.TODO(), r, dockertypes.ImageBuildOptions{
			Tags:        []string{srcImage},
			Remove:      true,
			ForceRemove: true,
			Context:     r,
		})
		require.NoError(t, err)
		defer buildResp.Body.Close()

		fmt.Println("Building container image", srcImage)
		_, err = io.Copy(os.Stdout, buildResp.Body)
		require.NoError(t, err)

		pushReader, err := dc.ImagePush(context.TODO(), srcImage, dockertypes.ImagePushOptions{RegistryAuth: "fake auth token"})
		require.NoError(t, err)
		defer pushReader.Close()

		fmt.Println("Pushing image to container registry")
		_, err = io.Copy(os.Stdout, pushReader)
		require.NoError(t, err)

		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_CONTAINER_IMAGE,
			App: &pb.TsuruApp{
				Name: "my-app",
			},
			SourceImage:       srcImage,
			DestinationImages: []string{baseRegistry(t, "app-my-app", "v1")},
			PushOptions:       &pb.PushOptions{InsecureRegistry: registryHTTP},
		}

		appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).
			Build(context.TODO(), req, os.Stdout)

		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			Procfile:  "web: my-server --addr 0.0.0.0:${PORT}\nworker: ./path/to/worker.sh --debug\n",
			TsuruYaml: "healthcheck:\n  path: /healthz\n  interval_seconds: 3\n  timeout_seconds: 1\n",
			ImageConfig: &pb.ContainerImageConfig{
				Cmd: []string{"sh"},
			},
		}, appFiles)
	})

	t.Run("container image without Tsuru app files (tsuru.yaml, Procfile)", func(t *testing.T) {
		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_DEPLOY_WITH_CONTAINER_IMAGE,
			App: &pb.TsuruApp{
				Name: "my-app",
			},
			SourceImage:       "nginx:1.22-alpine",
			DestinationImages: []string{baseRegistry(t, "app-my-app", "v2")},
			PushOptions:       &pb.PushOptions{InsecureRegistry: registryHTTP},
		}

		appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).
			Build(context.TODO(), req, os.Stdout)

		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			ImageConfig: &pb.ContainerImageConfig{
				Entrypoint:   []string{"/docker-entrypoint.sh"},
				Cmd:          []string{"nginx", "-g", "daemon off;"},
				ExposedPorts: []string{"80/tcp"},
			},
		}, appFiles)
	})
}

func appArchiveData(t *testing.T, dir string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	z, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	require.NoError(t, err)

	tw := tar.NewWriter(z)

	dirs, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, d := range dirs {
		fi, err := d.Info()
		require.NoError(t, err)

		if !fi.Mode().IsRegular() {
			continue
		}

		f, err := os.Open(filepath.Join(dir, d.Name()))
		require.NoError(t, err)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: d.Name(),
			Mode: int64(fi.Mode()),
			Size: fi.Size(),
		}))

		_, err = io.Copy(tw, f)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, z.Close())

	return buffer.Bytes()
}

func baseRegistry(t *testing.T, repository, tag string) string {
	t.Helper()
	return fmt.Sprintf("%s/%s/%s:%s", registryAddress, registryNamespace, repository, tag)
}

func newBuildKitClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(context.Background(), buildkitHost, client.WithFailFast())
	require.NoError(t, err)
	return c
}

func newDockerClient(t *testing.T) *dockerclient.Client {
	t.Helper()
	c, err := dockerclient.NewClientWithOpts(dockerclient.WithVersionFromEnv(), dockerclient.WithAPIVersionNegotiation(), dockerclient.WithHost(dockerHost))
	require.NoError(t, err)
	return c
}
