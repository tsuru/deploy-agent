// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/tsuru/deploy-agent/pkg/build"
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
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, IsTsuruYaml(tt.name))
		})
	}
}

func TestTsuruYamlCandidates_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		candidates TsuruYamlCandidates
		expected   string
	}{
		{},
		{
			candidates: TsuruYamlCandidates{
				"other.yaml":   "# My other.yaml file",
				"example.yaml": "# example.yaml",
			},
		},
		{
			candidates: TsuruYamlCandidates{
				"tsuru.yml":  "# Tsuru YAML from tsuru.yml",
				"tsuru.yaml": "-------------------------",
			},
			expected: "# Tsuru YAML from tsuru.yml",
		},
		{
			candidates: TsuruYamlCandidates{
				"tsuru.yaml": "# Tsuru YAML from tsuru.yaml",
				"app.yaml":   "----------------",
				"app.yml":    "----------------",
			},
			expected: "# Tsuru YAML from tsuru.yaml",
		},
		{
			candidates: TsuruYamlCandidates{
				"app.yaml": "----------------",
				"app.yml":  "# Tsuru YAML from app.yml",
			},
			expected: "# Tsuru YAML from app.yml",
		},
		{
			candidates: TsuruYamlCandidates{
				"app.yaml":   "# Tsuru YAML from app.yaml",
				"other.yaml": "--------------------",
			},
			expected: "# Tsuru YAML from app.yaml",
		},
	}

	for _, tt := range cases {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.candidates.String())
		})
	}
}

func TestExtractTsuruAppFilesFromAppSourceContext(t *testing.T) {
	cases := []struct {
		file          func(t *testing.T) io.Reader
		expected      *TsuruAppFiles
		expectedError string
	}{
		{
			file: func(t *testing.T) io.Reader {
				var buffer bytes.Buffer
				newTsuruAppSource(t, &buffer, map[string]string{
					"tsuru.yaml": "# Tsuru YAML",
					"app.yml":    "# Legacy Tsuru YAML",
					"Procfile":   `web: /path/to/server.sh --address 0.0.0.0:${PORT}`,
				})
				return &buffer
			},
			expected: &TsuruAppFiles{
				TsuruYaml: "# Tsuru YAML",
				Procfile:  `web: /path/to/server.sh --address 0.0.0.0:${PORT}`,
			},
		},

		{
			file: func(t *testing.T) io.Reader {
				return strings.NewReader(`not gzip`)
			},
			expectedError: "app source data must be a GZIP compressed file: unexpected EOF",
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

func newTsuruAppSource(t *testing.T, b *bytes.Buffer, files map[string]string) {
	z := gzip.NewWriter(b)
	defer z.Close()

	tr := tar.NewWriter(z)

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

COPY ./application.tar.gz /home/application/archive.tar.gz

RUN --mount=type=secret,id=tsuru-app-envvars,target=/var/run/secrets/envs.sh,uid=1000,gid=1000 \
    [ -f /var/run/secrets/envs.sh ] && . /var/run/secrets/envs.sh \
    && /var/lib/tsuru/deploy archive file:///home/application/archive.tar.gz \
    && :

WORKDIR /home/application/current
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

COPY ./application.tar.gz /home/application/archive.tar.gz

RUN --mount=type=secret,id=tsuru-app-envvars,target=/var/run/secrets/envs.sh,uid=1000,gid=1000 \
    [ -f /var/run/secrets/envs.sh ] && . /var/run/secrets/envs.sh \
    && /var/lib/tsuru/deploy archive file:///home/application/archive.tar.gz \
    && { mkdir -p /tmp/foo; } \
    && { echo "Hello world" > /tmp/foo/bar; } \
    && :

WORKDIR /home/application/current
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
