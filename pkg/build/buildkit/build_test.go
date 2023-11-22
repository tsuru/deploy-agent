// Copyright 2023 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildkit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
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
	"github.com/tsuru/deploy-agent/pkg/util"
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
	destImage := baseRegistry(t, "python", "")

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
		Data:              compressGZIP(t, "./testdata/python/"),
		PushOptions:       &pb.PushOptions{InsecureRegistry: registryHTTP},
	}

	bc := newBuildKitClient(t)
	defer bc.Close()

	appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).
		Build(context.TODO(), req, os.Stdout)

	require.NoError(t, err)
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
	destImage := baseRegistry(t, "my-static-app", "")

	req := &pb.BuildRequest{
		Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_SOURCE_UPLOAD,
		App: &pb.TsuruApp{
			Name: "my-app",
		},
		SourceImage:       "tsuru/static:2.3",
		DestinationImages: []string{destImage},
		Data:              compressGZIP(t, "./testdata/static/"),
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

		data := compressGZIP(t, "./testdata/container_image/")
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
			DestinationImages: []string{baseRegistry(t, "app-my-app", "")},
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
			DestinationImages: []string{baseRegistry(t, "app-my-app", "")},
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

	t.Run("container image without Tsuru app files (tsuru.yaml, Procfile) + job image push", func(t *testing.T) {
		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_JOB_CREATE_WITH_CONTAINER_IMAGE,
			Job: &pb.TsuruJob{
				Name: "my-job",
			},
			SourceImage:       "nginx:1.22-alpine",
			DestinationImages: []string{baseRegistry(t, "job-my-job", "")},
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

func TestBuildKit_Build_FromContainerFile(t *testing.T) {
	bc := newBuildKitClient(t)
	defer bc.Close()

	t.Run("Dockerfile w/ build context (adds tsuru.yaml and Procfile)", func(t *testing.T) {
		destImage := baseRegistry(t, "my-app", "")

		dockerfile, err := os.ReadFile("./testdata/container_file/Dockerfile")
		require.NoError(t, err)

		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_CONTAINER_FILE,
			App: &pb.TsuruApp{
				Name: "my-app",
			},
			DestinationImages: []string{destImage},
			Containerfile:     string(dockerfile),
			Data:              compressGZIP(t, "./testdata/container_file/"),
			PushOptions: &pb.PushOptions{
				InsecureRegistry: registryHTTP,
			},
		}

		appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).Build(context.TODO(), req, os.Stdout)
		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			Procfile: "web: /path/to/webserver.sh --port 8888\nworker: /path/to/worker.sh\n",
			TsuruYaml: `healthcheck:
  command:
  - /usr/bin/true

hooks:
  restart:
    before:
    - /path/to/pre_start.sh
    after:
    - /path/to/shutdown.sh

kubernetes:
  groups:
    my-app:
      web:
        ports:
        - name: http
          port: 80
          target_port: 8888
          protocol: TCP
`,
			ImageConfig: &pb.ContainerImageConfig{
				Cmd:        []string{"/bin/sh"},
				WorkingDir: "/app/user",
			},
		}, appFiles)
	})

	t.Run("Dockerfile mounting the app's env vars", func(t *testing.T) {
		destImage := baseRegistry(t, "my-app", "")

		dockerfile := `FROM busybox:latest

RUN --mount=type=secret,id=tsuru-app-envvars,target=/var/run/secrets/envs.sh \
    . /var/run/secrets/envs.sh \
    && echo ${MY_ENV_VAR} > /tmp/envs \
    && echo ${DATABASE_PASSWORD} >> /tmp/envs

ENV MY_ANOTHER_VAR="another var"
`

		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_CONTAINER_FILE,
			App: &pb.TsuruApp{
				Name: "my-app",
				EnvVars: map[string]string{
					"MY_ENV_VAR":        "hello world",
					"DATABASE_PASSWORD": "aw3some`p4ss!",
				},
			},
			DestinationImages: []string{destImage},
			Containerfile:     string(dockerfile),
			PushOptions: &pb.PushOptions{
				InsecureRegistry: registryHTTP,
			},
		}

		appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).Build(context.TODO(), req, os.Stdout)
		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			ImageConfig: &pb.ContainerImageConfig{
				Cmd: []string{"sh"},
			},
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
		t.Run("should not store the env vars in the container image manifest", func(t *testing.T) {
			is, _, err := dc.ImageInspectWithRaw(context.TODO(), destImage)
			require.NoError(t, err)
			require.NotNil(t, is.Config)
			assert.NotEmpty(t, is.Config.Env)
			for _, env := range is.Config.Env {
				assert.False(t, strings.HasPrefix(env, "MY_ENV_VAR="), "Env MY_ENV_VAR should not be exported to image manifest")
				assert.False(t, strings.HasPrefix(env, "DATABASE_PASSWORD="), "Env DATABASE_PASSWORD shold not be exported to image manifest")

				if strings.HasPrefix(env, "MY_ANOTHER_VAR=") {
					assert.Equal(t, "MY_ANOTHER_VAR=another var", env)
				}
			}
		})
		t.Run("should be able to see env vars during the build", func(t *testing.T) {
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

			execCreateResp, err := dc.ContainerExecCreate(context.TODO(), containerID, dockertypes.ExecConfig{
				Tty:          true,
				AttachStderr: true,
				AttachStdout: true,
				Cmd:          []string{"cat", "/tmp/envs"},
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
			assert.Equal(t, "hello world\r\naw3some`p4ss!\r\n", stdout.String())
			assert.Empty(t, stderr.String())
		})
	})
	t.Run("Job build with Dockerfile", func(t *testing.T) {
		destImage := baseRegistry(t, "my-job", "")

		dockerfile := `FROM busybox:latest
	
	RUN --mount=type=secret,id=tsuru-job-envvars,target=/var/run/secrets/envs.sh \
		. /var/run/secrets/envs.sh \
		&& echo ${MY_ENV_VAR} > /tmp/envs \
		&& echo ${DATABASE_PASSWORD} >> /tmp/envs
	
	ENV MY_ANOTHER_VAR="another var"
	`

		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_CONTAINER_FILE,
			Job: &pb.TsuruJob{
				Name: "my-job",
				EnvVars: map[string]string{
					"MY_ENV_VAR":        "hello world",
					"DATABASE_PASSWORD": "aw3some`p4ss!",
				},
			},
			DestinationImages: []string{destImage},
			Containerfile:     string(dockerfile),
			PushOptions: &pb.PushOptions{
				InsecureRegistry: registryHTTP,
			},
		}

		jobFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).Build(context.TODO(), req, os.Stdout)
		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			ImageConfig: &pb.ContainerImageConfig{
				Cmd: []string{"sh"},
			},
		}, jobFiles)

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
	})

	t.Run("neither Procfile nor tsuru.yaml, should use command from image manifest", func(t *testing.T) {
		destImage := baseRegistry(t, "my-app", "")

		dockerfile := `FROM busybox

EXPOSE 8080/tcp

ENTRYPOINT ["/path/to/my/server.sh"]

CMD ["--port", "8080"]
`

		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_CONTAINER_FILE,
			App: &pb.TsuruApp{
				Name: "my-app",
			},
			DestinationImages: []string{destImage},
			Containerfile:     string(dockerfile),
			PushOptions: &pb.PushOptions{
				InsecureRegistry: registryHTTP,
			},
		}

		appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).Build(context.TODO(), req, os.Stdout)
		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			ImageConfig: &pb.ContainerImageConfig{
				Entrypoint:   []string{"/path/to/my/server.sh"},
				Cmd:          []string{"--port", "8080"},
				ExposedPorts: []string{"8080/tcp"},
			},
		}, appFiles)
	})

	t.Run("using a different working directory, should get Procfile and Tsuru YAML from that", func(t *testing.T) {
		destImage := baseRegistry(t, "my-app", "")

		dockerfile := `FROM busybox

RUN set -xef \
    && mkdir -p /var/my-app \
    && echo "web: /path/to/server.sh --port 8888" > /var/my-app/Procfile \
    && echo -e "healthcheck:\n  path: /healthz\n" > /var/my-app/tsuru.yaml

WORKDIR /var/my-app

EXPOSE 8888/tcp
`

		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_CONTAINER_FILE,
			App: &pb.TsuruApp{
				Name: "my-app",
			},
			DestinationImages: []string{destImage},
			Containerfile:     string(dockerfile),
			PushOptions: &pb.PushOptions{
				InsecureRegistry: registryHTTP,
			},
		}

		appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).Build(context.TODO(), req, os.Stdout)
		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			Procfile:  "web: /path/to/server.sh --port 8888\n",
			TsuruYaml: "healthcheck:\n  path: /healthz\n\n",
			ImageConfig: &pb.ContainerImageConfig{
				Cmd:          []string{"sh"},
				ExposedPorts: []string{"8888/tcp"},
				WorkingDir:   "/var/my-app",
			},
		}, appFiles)
	})

	t.Run("multiple exposed ports, should ensure the ascending order of ports", func(t *testing.T) {
		destImage := baseRegistry(t, "my-app", "")

		dockerfile := `FROM busybox

EXPOSE 100/udp 53/udp 443/udp
EXPOSE 8080/tcp 80/tcp 8000/tcp 9090 8888
`
		req := &pb.BuildRequest{
			Kind: pb.BuildKind_BUILD_KIND_APP_BUILD_WITH_CONTAINER_FILE,
			App: &pb.TsuruApp{
				Name: "my-app",
			},
			DestinationImages: []string{destImage},
			Containerfile:     string(dockerfile),
			PushOptions: &pb.PushOptions{
				InsecureRegistry: registryHTTP,
			},
		}

		appFiles, err := NewBuildKit(bc, BuildKitOptions{TempDir: t.TempDir()}).Build(context.TODO(), req, os.Stdout)
		require.NoError(t, err)
		assert.Equal(t, &pb.TsuruConfig{
			ImageConfig: &pb.ContainerImageConfig{
				Cmd:          []string{"sh"},
				ExposedPorts: []string{"53/udp", "80/tcp", "100/udp", "443/udp", "8000/tcp", "8080/tcp", "8888/tcp", "9090/tcp"},
			},
		}, appFiles)
	})
}

func compressGZIP(t *testing.T, path string) []byte {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, util.CompressGZIPFile(context.TODO(), &data, path))
	return data.Bytes()
}

func baseRegistry(t *testing.T, repository, tag string) string {
	t.Helper()

	if tag == "" {
		tag = fmt.Sprintf("%x", sha256.Sum256([]byte(t.Name())))
	}

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
