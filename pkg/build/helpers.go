// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"text/template"

	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

var (
	ProcfileName   = "Procfile"
	TsuruYamlNames = []string{"tsuru.yml", "tsuru.yaml", "app.yml", "app.yaml"}
)

func IsTsuruYaml(name string) bool {
	for _, n := range TsuruYamlNames {
		if n == name {
			return true
		}
	}

	return false
}

type TsuruYamlCandidates map[string]string

func (c TsuruYamlCandidates) String() string {
	for _, n := range TsuruYamlNames {
		if s, found := c[n]; found {
			return s
		}
	}

	return ""
}

type TsuruAppFiles struct {
	Procfile  string
	TsuruYaml string
}

func ExtractTsuruAppFilesFromAppSourceContext(ctx context.Context, r io.Reader) (*TsuruAppFiles, error) {
	if err := ctx.Err(); err != nil { // context deadline exceeded
		return nil, err
	}

	z, err := gzip.NewReader(r)
	if err != nil { // not gzip file
		return nil, fmt.Errorf("app source data must be a GZIP compressed file: %w", err)
	}
	defer z.Close()

	t := tar.NewReader(z)

	var procfile string
	tsuruYaml := make(TsuruYamlCandidates)

	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read next file in the tarball: %w", err)
		}

		if h.Typeflag != tar.TypeReg { // not a regular file, skipping...
			continue
		}

		name := strings.TrimPrefix(h.Name, "./") // e.g. "./Procfile" to "Procfile"
		if IsTsuruYaml(name) {
			data, err := io.ReadAll(t)
			if err != nil {
				return nil, err
			}

			tsuruYaml[name] = string(data)
		}

		if name == ProcfileName {
			data, err := io.ReadAll(t)
			if err != nil {
				return nil, err
			}

			procfile = string(data)
		}
	}

	return &TsuruAppFiles{
		Procfile:  procfile,
		TsuruYaml: tsuruYaml.String(),
	}, nil
}

type BuildContainerfileParams struct {
	Image      string
	BuildHooks []string
}

func BuildContainerfile(p BuildContainerfileParams) (string, error) {
	var w bytes.Buffer
	if err := containerfileTemplate.Execute(&w, p); err != nil {
		return "", err
	}

	return w.String(), nil
}

var containerfileTemplate = template.Must(template.New("containerfile").Parse(`
FROM {{ .Image }}

COPY ./application.tar.gz /home/application/archive.tar.gz

RUN --mount=type=secret,id=tsuru-app-envvars,target=/var/run/secrets/envs.sh,uid=1000,gid=1000 \
    [ -f /var/run/secrets/envs.sh ] && . /var/run/secrets/envs.sh \
    && /var/lib/tsuru/deploy archive file:///home/application/archive.tar.gz \
{{- range $_, $hook := .BuildHooks }}
    && { {{ . }}; } \
{{- end }}
    && :

WORKDIR /home/application/current
`))

type BuildResponseOutputWriter struct {
	stream pb.Build_BuildServer
	mu     sync.Mutex
}

func (w *BuildResponseOutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(p) == 0 {
		return 0, nil
	}

	return len(p), w.stream.Send(&pb.BuildResponse{Data: &pb.BuildResponse_Output{Output: string(p)}})
}

func (w *BuildResponseOutputWriter) Read(p []byte) (int, error) { // required to implement console.File
	return 0, nil
}

func (w *BuildResponseOutputWriter) Close() error { // required to implement console.File
	return nil
}

func (w *BuildResponseOutputWriter) Fd() uintptr { // required to implement console.File
	return uintptr(0)
}

func (w *BuildResponseOutputWriter) Name() string { // required to implement console.File
	return ""
}
