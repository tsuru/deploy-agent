// Copyright 2022 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	tsuruprovtypes "github.com/tsuru/tsuru/types/provision"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	pb "github.com/tsuru/deploy-agent/v2/api/v1alpha1"
)

var _ pb.BuildServer = (*Docker)(nil)

type DockerOptions struct {
	TempDir string
}

func NewDocker(dc *client.Client, opts DockerOptions) *Docker {
	return &Docker{Client: dc, opts: opts}
}

type Docker struct {
	*pb.UnimplementedBuildServer
	*client.Client

	opts DockerOptions
}

func (d *Docker) Build(req *pb.BuildRequest, stream pb.Build_BuildServer) error {
	fmt.Println("Build RPC called")
	defer fmt.Println("Finishing Build RPC call")

	ctx := stream.Context()
	if err := ctx.Err(); err != nil { // e.g. context deadline exceeded
		return err
	}

	// TODO: check if mandatory field were provided
	tsuruFiles, err := ExtractTsuruAppFilesFromAppSourceContext(ctx, bytes.NewReader(req.Data))
	if err != nil {
		return err
	}

	if err = stream.Send(&pb.BuildResponse{
		Data: &pb.BuildResponse_TsuruConfig{
			TsuruConfig: &pb.TsuruConfig{
				Procfile:  tsuruFiles.Procfile,
				TsuruYaml: tsuruFiles.TsuruYaml,
			},
		}}); err != nil {
		return status.Errorf(codes.Unknown, "failed to send tsuru app files: %s", err)
	}

	w := &BuildResponseOutputWriter{stream}

	if err = d.build(ctx, req, tsuruFiles, bytes.NewReader(req.Data), int64(len(req.Data)), w); err != nil {
		return status.Errorf(codes.Internal, "failed to build container image: %s", err)
	}

	if err = d.push(ctx, req.DestinationImages, w); err != nil {
		return status.Errorf(codes.Internal, "failed to push container image(s) to registry: %s", err)
	}

	fmt.Fprintln(w, "OK")

	return nil
}

func (d *Docker) build(ctx context.Context, req *pb.BuildRequest, tsuruAppFiles *TsuruAppFiles, appData io.Reader, appDataSize int64, w io.Writer) error {
	if err := ctx.Err(); err != nil { // e.g. context deadline exceeded
		return err
	}

	if w == nil {
		w = io.Discard
	}

	dockerfile, err := generateContainerfile(req.SourceImage, tsuruAppFiles)
	if err != nil {
		return err
	}

	var dockerBuildContext bytes.Buffer
	if err = generateDockerBuildContext(&dockerBuildContext, dockerfile, appData, appDataSize); err != nil {
		return err
	}

	resp, err := d.Client.ImageBuild(ctx, &dockerBuildContext, types.ImageBuildOptions{
		Version:    types.BuilderBuildKit,
		Tags:       req.DestinationImages,
		Remove:     true,
		Dockerfile: "Dockerfile",
		Context:    &dockerBuildContext,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(w, resp.Body)
	if errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

func (d *Docker) push(ctx context.Context, imageNames []string, w io.Writer) error {
	if err := ctx.Err(); err != nil { // e.g. context deadline exceeded, context cancelled
		return err
	}

	if w == nil {
		w = io.Discard
	}

	for _, image := range imageNames {
		fmt.Fprintf(w, "Pushing container image %s to registry...\n", image)

		r, err := d.Client.ImagePush(ctx, image, types.ImagePushOptions{})
		if err != nil {
			return err
		}
		defer r.Close()

		_, err = io.Copy(w, r)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}

	return nil
}

func generateContainerfile(image string, tsuruAppFiles *TsuruAppFiles) (string, error) {
	var tsuruYaml tsuruprovtypes.TsuruYamlData
	if tsuruAppFiles != nil {
		if err := yaml.Unmarshal([]byte(tsuruAppFiles.TsuruYaml), &tsuruYaml); err != nil {
			return "", err
		}
	}

	var buildHooks []string
	if hooks := tsuruYaml.Hooks; hooks != nil {
		buildHooks = hooks.Build
	}

	return BuildContainerfile(BuildContainerfileParams{
		Image:      image,
		BuildHooks: buildHooks,
	})
}

func generateDockerBuildContext(dst io.Writer, dockerfile string, appData io.Reader, appDataSize int64) error {
	if dst == nil {
		return fmt.Errorf("writer cannot be nil")
	}

	ww := tar.NewWriter(dst)

	if err := ww.WriteHeader(&tar.Header{
		Name: "Dockerfile",
		Mode: 0744,
		Size: int64(len(dockerfile)),
	}); err != nil {
		return err
	}

	if _, err := io.WriteString(ww, dockerfile); err != nil {
		return err
	}

	if err := ww.WriteHeader(&tar.Header{
		Name: "application.tar.gz",
		Mode: 0744,
		Size: appDataSize,
	}); err != nil {
		return err
	}

	if _, err := io.Copy(ww, appData); err != nil {
		return err
	}

	return ww.Close()
}

type BuildResponseOutputWriter struct {
	stream pb.Build_BuildServer
}

func (w *BuildResponseOutputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	return len(p), w.stream.Send(&pb.BuildResponse{Data: &pb.BuildResponse_Output{Output: string(p)}})
}
