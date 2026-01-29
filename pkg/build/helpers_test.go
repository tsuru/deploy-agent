// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/tsuru/deploy-agent/pkg/build"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

func TestIsTsuruYaml(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		expected bool
	}{
		{},
		{name: "tsuru.yaml", expected: true},
		{name: "tsuru.yml", expected: true},
		{name: "app.yml", expected: true},
		{name: "app.yaml", expected: true},
		{name: "other.yaml"},
		{name: "not.txt"},
		{name: "./tsuru.yaml", expected: true},
		{name: "/home/application/current/tsuru.yaml", expected: true},
		{name: "/home/application/current/other.yaml"},
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, IsTsuruYaml(tt.name))
		})
	}
}

func TestTsuruYamlCandidates_Pick(t *testing.T) {
	t.Parallel()

	cases := []struct {
		candidates TsuruYamlCandidates
		workingDir string
		expected   string
	}{
		{},
		{
			candidates: TsuruYamlCandidates{
				"/home/application/current/other.yaml":   "# My other.yaml file",
				"/home/application/current/example.yaml": "# example.yaml",
			},
		},
		{
			candidates: TsuruYamlCandidates{
				"/home/application/current/tsuru.yml":  "# Tsuru YAML from tsuru.yml",
				"/home/application/current/tsuru.yaml": "-------------------------",
			},
			expected: "# Tsuru YAML from tsuru.yml",
		},
		{
			candidates: TsuruYamlCandidates{
				"/home/application/current/tsuru.yaml": "# Tsuru YAML from tsuru.yaml",
				"/home/application/current/app.yaml":   "----------------",
				"/home/application/current/app.yml":    "----------------",
			},
			expected: "# Tsuru YAML from tsuru.yaml",
		},
		{
			candidates: TsuruYamlCandidates{
				"/home/application/current/app.yaml": "----------------",
				"/home/application/current/app.yml":  "# Tsuru YAML from app.yml",
			},
			expected: "# Tsuru YAML from app.yml",
		},
		{
			candidates: TsuruYamlCandidates{
				"/home/application/current/app.yaml":   "# Tsuru YAML from app.yaml",
				"/home/application/current/other.yaml": "--------------------",
			},
			expected: "# Tsuru YAML from app.yaml",
		},
		{
			candidates: TsuruYamlCandidates{
				"/home/application/current/tsuru.yaml": "# Tsuru YAML from tsuru.yaml",
				"/app/user/tsuru.yaml":                 "--------------------",
				"/tsuru.yaml":                          "--------------------",
			},
			expected: "# Tsuru YAML from tsuru.yaml",
		},
		{
			candidates: TsuruYamlCandidates{
				"/app/user/tsuru.yaml": "# Tsuru YAML from tsuru.yaml",
				"/tsuru.yaml":          "--------------------",
			},
			expected: "# Tsuru YAML from tsuru.yaml",
		},
		{
			candidates: TsuruYamlCandidates{
				"/tsuru.yaml": "# Tsuru YAML from tsuru.yaml",
				"/other.yaml": "--------------------",
			},
			expected: "# Tsuru YAML from tsuru.yaml",
		},
		{
			workingDir: "/var/www/html",
			candidates: TsuruYamlCandidates{
				"/var/www/html/tsuru.yaml":             "# Tsuru YAML from tsuru.yaml",
				"/home/application/current/tsuru.yaml": "--------------------",
				"/app/user/tsuru.yaml":                 "--------------------",
				"/tsuru.yaml":                          "--------------------",
			},
			expected: "# Tsuru YAML from tsuru.yaml",
		},
		{
			workingDir: "/not/found",
			candidates: TsuruYamlCandidates{
				"/var/www/html/tsuru.yaml":             "--------------------",
				"/home/application/current/tsuru.yaml": "# Tsuru YAML from tsuru.yaml",
				"/app/user/tsuru.yaml":                 "--------------------",
				"/tsuru.yaml":                          "--------------------",
			},
			expected: "# Tsuru YAML from tsuru.yaml",
		},
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.candidates.Pick(tt.workingDir))
		})
	}
}

func TestProcfileCandidates_Pick(t *testing.T) {
	t.Parallel()

	cases := []struct {
		candidates ProcfileCandidates
		workingDir string
		expected   string
	}{
		{},
		{
			candidates: ProcfileCandidates{
				"/home/application/current/Procfile": "web: ./path/to/server.sh --port ${PORT}",
				"/app/user/Procfile":                 "--------------------",
				"/Procfile":                          "--------------------",
			},
			expected: "web: ./path/to/server.sh --port ${PORT}",
		},
		{
			candidates: ProcfileCandidates{
				"/app/user/Procfile": "web: ./path/to/server.sh --port ${PORT}",
				"/Procfile":          "--------------------",
			},
			expected: "web: ./path/to/server.sh --port ${PORT}",
		},
		{
			candidates: ProcfileCandidates{
				"/Procfile":     "web: ./path/to/server.sh --port ${PORT}",
				"/tmp/Procfile": "--------------------",
			},
			expected: "web: ./path/to/server.sh --port ${PORT}",
		},
		{
			candidates: ProcfileCandidates{
				"/tmp/Procfile":                           "--------------------",
				"/app/user/demo/Procfile":                 "--------------------",
				"/home/application/current/demo/Procfile": "--------------------",
			},
		},
		{
			workingDir: "/var/www/html",
			candidates: ProcfileCandidates{
				"/var/www/html/Procfile":             "web: ./path/to/server.sh --port ${PORT}",
				"/home/application/current/Procfile": "--------------------",
				"/app/user/Procfile":                 "--------------------",
				"/Procfile":                          "--------------------",
			},
			expected: "web: ./path/to/server.sh --port ${PORT}",
		},
		{
			workingDir: "/not/found",
			candidates: ProcfileCandidates{
				"/var/www/html/Procfile":             "--------------------",
				"/home/application/current/Procfile": "web: ./path/to/server.sh --port ${PORT}",
				"/app/user/Procfile":                 "--------------------",
				"/Procfile":                          "--------------------",
			},
			expected: "web: ./path/to/server.sh --port ${PORT}",
		},
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.candidates.Pick(tt.workingDir))
		})
	}
}

func TestExtractTsuruAppFilesFromAppSourceContext(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file          func(t *testing.T) io.Reader
		expected      *pb.TsuruConfig
		expectedError string
	}{
		{
			file: func(t *testing.T) io.Reader {
				return strings.NewReader(`not gzip`)
			},
			expectedError: "app source data must be a GZIP compressed file: unexpected EOF",
		},

		{
			file: func(t *testing.T) io.Reader {
				var b bytes.Buffer
				z := gzip.NewWriter(&b)
				fmt.Fprintln(z, "gzip but not tarball")
				z.Close()
				return &b
			},
			expectedError: "failed to read next file in the tarball: unexpected EOF",
		},

		{
			file: func(t *testing.T) io.Reader {
				var buffer bytes.Buffer
				newTsuruAppSource(t, &buffer, map[string]string{
					"Procfile":        `web: /path/to/server.sh --address 0.0.0.0:${PORT}`,
					"tsuru.yaml":      "# Tsuru YAML",
					"app.yml":         "# Legacy Tsuru YAML",
					"demo/tsuru.yaml": "# Other Tsuru YAML",
					"demo/Procfile":   "web: ./path/to/other.sh\nworker: ./my/worker.sh\n",
				})
				return &buffer
			},
			expected: &pb.TsuruConfig{
				TsuruYaml: "# Tsuru YAML",
				Procfile:  `web: /path/to/server.sh --address 0.0.0.0:${PORT}`,
			},
		},
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			require.NotNil(t, tt.file)
			tsuruFiles, err := ExtractTsuruAppFilesFromAppSourceContext(context.TODO(), tt.file(t))
			if err != nil {
				require.EqualError(t, err, tt.expectedError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tsuruFiles, tt.expected)
		})
	}
}

func TestExtractTsuruAppFilesFromContainerImageTarball(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file          func(t *testing.T) io.Reader
		expected      *pb.TsuruConfig
		expectedError string
	}{
		{
			file: func(t *testing.T) io.Reader {
				return strings.NewReader(`not tarball`)
			},
			expectedError: "failed to read next file in the tarball: unexpected EOF",
		},

		{
			file: func(t *testing.T) io.Reader {
				var buffer bytes.Buffer
				makeTarballFile(t, &buffer, map[string]string{
					"/home/application/current/Procfile":      "Awesome Procfile",
					"/home/application/current/demo/Procfile": "bad Procfile",
					"/app/user/Procfile":                      "bad Procfile",
					"/tmp/Procfile":                           "bad Procfile",
					"/Procfile":                               "bad Procfile",

					"/home/application/current/tsuru.yml":      "Awesome Tsuru YAML",
					"/home/application/current/demo/tsuru.yml": "bad Tsuru YAML",
					"/app/user/tsuru.yml":                      "bad Tsuru YAML",
					"/tsuru.yml":                               "bad Tsuru YAML",
				})
				return &buffer
			},
			expected: &pb.TsuruConfig{
				TsuruYaml: "Awesome Tsuru YAML",
				Procfile:  `Awesome Procfile`,
			},
		},
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			require.NotNil(t, tt.file)
			tsuruFiles, err := ExtractTsuruAppFilesFromContainerImageTarball(context.TODO(), tt.file(t), "")
			if err != nil {
				require.EqualError(t, err, tt.expectedError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tsuruFiles, tt.expected)
		})
	}
}

func newTsuruAppSource(t *testing.T, w io.Writer, files map[string]string) {
	t.Helper()

	z := gzip.NewWriter(w)
	defer z.Close()

	makeTarballFile(t, z, files)
}

func makeTarballFile(t *testing.T, w io.Writer, files map[string]string) {
	t.Helper()

	tr := tar.NewWriter(w)
	defer tr.Close()

	for name, content := range files {
		err := tr.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0744,
			Size: int64(len(content)),
		})
		require.NoError(t, err)

		_, err = io.WriteString(tr, content)
		require.NoError(t, err)
	}
}

func TestBuildContainerfile(t *testing.T) {
	cases := []struct {
		expected string
		params   BuildContainerfileParams
	}{
		{
			params: BuildContainerfileParams{
				Image: "tsuru/scratch:latest",
			},
			expected: `
FROM tsuru/scratch:latest

WORKDIR /home/application/current

COPY ./application.tar.gz /home/application/archive.tar.gz

ARG tsuru_deploy_cache=1

RUN --mount=type=secret,id=tsuru-app-envvars,target=/var/run/secrets/envs.sh,uid=1000,gid=1000 \
    [ -f /var/run/secrets/envs.sh ] && . /var/run/secrets/envs.sh \
    && [ -f ~/.profile ] && . ~/.profile \
    && /var/lib/tsuru/deploy archive file:///home/application/archive.tar.gz \
    && :
`,
		},
		{
			params: BuildContainerfileParams{
				Image: "tsuru/scratch:latest",
				BuildHooks: []string{
					"mkdir -p /tmp/foo",
					`echo "Hello world" > /tmp/foo/bar`,
				},
			},
			expected: `
FROM tsuru/scratch:latest

WORKDIR /home/application/current

COPY ./application.tar.gz /home/application/archive.tar.gz

ARG tsuru_deploy_cache=1

RUN --mount=type=secret,id=tsuru-app-envvars,target=/var/run/secrets/envs.sh,uid=1000,gid=1000 \
    [ -f /var/run/secrets/envs.sh ] && . /var/run/secrets/envs.sh \
    && [ -f ~/.profile ] && . ~/.profile \
    && /var/lib/tsuru/deploy archive file:///home/application/archive.tar.gz \
    && { sh -lc 'mkdir -p /tmp/foo'; } \
    && { sh -lc 'echo "Hello world" > /tmp/foo/bar'; } \
    && :
`,
		},
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			containerfile, err := BuildContainerfile(tt.params)
			require.NoError(t, err)
			assert.Equal(t, containerfile, tt.expected)
		})
	}
}

func TestSortExposedPorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ports    map[string]struct{}
		expected []string
	}{
		{},
		{
			ports:    map[string]struct{}{"8888/tcp": {}},
			expected: []string{"8888/tcp"},
		},
		{
			ports:    map[string]struct{}{"8888/tcp": {}, "80/tcp": {}, "53/udp": {}, "80/udp": {}, "8000/tcp": {}},
			expected: []string{"53/udp", "80/tcp", "80/udp", "8000/tcp", "8888/tcp"},
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, SortExposedPorts(tt.ports))
		})
	}
}
