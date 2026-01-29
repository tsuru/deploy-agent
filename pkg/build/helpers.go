// Copyright 2026 tsuru authors. All rights reserved.
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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/alessio/shellescape"

	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
)

func IsTsuruYaml(filename string) bool {
	baseName := filepath.Base(filename)

	for _, n := range TsuruYamlNames {
		if n == baseName {
			return true
		}
	}

	return false
}

type TsuruYamlCandidates map[string]string

func (c TsuruYamlCandidates) Pick(workingDir string) string {
	dirs := make([]string, 0, (len(TsuruConfigDirs) + 1))

	if workingDir != "" {
		dirs = append(dirs, workingDir) // added first to get higher precedence
	}

	dirs = append(dirs, TsuruConfigDirs...)

	for _, dir := range dirs {
		for _, baseName := range TsuruYamlNames {
			filename := filepath.Join(dir, baseName)
			if s, found := c[filename]; found {
				return s
			}
		}
	}

	return ""
}

func IsProcfile(filename string) bool {
	return filepath.Base(filename) == ProcfileName
}

type ProcfileCandidates map[string]string

func (c ProcfileCandidates) Pick(workingDir string) string {
	dirs := make([]string, 0, (len(TsuruConfigDirs) + 1))

	if workingDir != "" {
		dirs = append(dirs, workingDir) // added first to get higher precedence
	}

	dirs = append(dirs, TsuruConfigDirs...)

	for _, dir := range dirs {
		filename := filepath.Join(dir, ProcfileName)
		if s, found := c[filename]; found {
			return s
		}
	}

	return ""
}

func ExtractTsuruAppFilesFromAppSourceContext(ctx context.Context, r io.Reader) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil { // context deadline exceeded
		return nil, err
	}

	z, err := gzip.NewReader(r)
	if err != nil { // not gzip file
		return nil, fmt.Errorf("app source data must be a GZIP compressed file: %w", err)
	}
	defer z.Close()

	t := tar.NewReader(z)

	procfile := make(ProcfileCandidates)
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

		filename := filepath.Join(DefaultTsuruPlatformWorkingDir, h.Name) // nolint

		if err = copyTsuruYamlToCandidate(filename, t, tsuruYaml); err != nil {
			return nil, err
		}

		if err = copyProcfileToCandidate(filename, t, procfile); err != nil {
			return nil, err
		}
	}

	return &pb.TsuruConfig{
		Procfile:  procfile.Pick(DefaultTsuruPlatformWorkingDir),
		TsuruYaml: tsuruYaml.Pick(DefaultTsuruPlatformWorkingDir),
	}, nil
}

func ExtractTsuruAppFilesFromContainerImageTarball(ctx context.Context, r io.Reader, workingDir string) (*pb.TsuruConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	procfile := make(ProcfileCandidates)
	tsuruYaml := make(TsuruYamlCandidates)

	t := tar.NewReader(r)
	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read next file in the tarball: %w", err)
		}

		if h.Typeflag != tar.TypeReg {
			continue
		}

		filename := filepath.Join(string(filepath.Separator), h.Name) // nolint

		if err = copyTsuruYamlToCandidate(filename, t, tsuruYaml); err != nil {
			return nil, err
		}

		if err = copyProcfileToCandidate(filename, t, procfile); err != nil {
			return nil, err
		}
	}

	return &pb.TsuruConfig{
		Procfile:  procfile.Pick(workingDir),
		TsuruYaml: tsuruYaml.Pick(workingDir),
	}, nil
}

func copyTsuruYamlToCandidate(filename string, r io.Reader, dst TsuruYamlCandidates) error {
	if !IsTsuruYaml(filename) {
		return nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	dst[filename] = string(data)
	return nil
}

func copyProcfileToCandidate(filename string, r io.Reader, dst ProcfileCandidates) error {
	if !IsProcfile(filename) {
		return nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	dst[filename] = string(data)
	return nil
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

var containerfileTemplate = template.Must(template.New("containerfile").
	Funcs(template.FuncMap{
		"shellQuote": shellescape.Quote,
	}).
	Parse(`
FROM {{ .Image }}

WORKDIR /home/application/current

COPY ./application.tar.gz /home/application/archive.tar.gz

ARG tsuru_deploy_cache=1

RUN --mount=type=secret,id=tsuru-app-envvars,target=/var/run/secrets/envs.sh,uid=1000,gid=1000 \
    [ -f /var/run/secrets/envs.sh ] && . /var/run/secrets/envs.sh \
    && [ -f ~/.profile ] && . ~/.profile \
    && /var/lib/tsuru/deploy archive file:///home/application/archive.tar.gz \
{{- range $_, $hook := .BuildHooks }}
    && { sh -lc {{ shellQuote . }}; } \
{{- end }}
    && :
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

func SortExposedPorts(ports map[string]struct{}) []string {
	var ps []string
	for p := range ports {
		ps = append(ps, p)
	}

	sort.Slice(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]

		// Example of exposed port: "8080/tcp"
		portAStr, protoAStr, _ := strings.Cut(a, "/")
		portBStr, protoBStr, _ := strings.Cut(b, "/")

		portA, _ := strconv.Atoi(portAStr)
		portB, _ := strconv.Atoi(portBStr)

		if portA == portB {
			return protoAStr < protoBStr
		}

		return portA < portB
	})

	return ps
}

func GetRegistry(image string) string {
	defaultRegistry := "docker.io"
	if !strings.Contains(image, "/") {
		return defaultRegistry
	}

	registry := strings.Split(image, "/")[0]
	if strings.Contains(registry, ".") || strings.Contains(registry, ":") {
		return registry
	}

	return defaultRegistry
}
