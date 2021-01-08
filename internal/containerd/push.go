/*
   Copyright The containerd Authors.
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at
       http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package containerd

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
)

// This file mostly contains unexported code copied from containerd/containerd.
// Ref:
// https://github.com/containerd/containerd/blob/a4f4a4311022e9dddeff5ad6d633847aaa32d4c4/cmd/ctr/commands/images/push.go
// The copyright of this file belongs to containerd authors as stated on the
// header above. The copied code is useful for displaying progress information
// during image push operations.

func pushWithProgress(ctx context.Context, client *containerd.Client, ref string, target ocispec.Descriptor, tracker docker.StatusTracker, resolver remotes.Resolver, w io.Writer) error {
	ongoing := newPushJobs(tracker)

	eg, ctx := errgroup.WithContext(ctx)

	// used to notify the progress writer
	doneCh := make(chan struct{})

	eg.Go(func() error {
		defer close(doneCh)

		jobHandler := images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
			ongoing.add(remotes.MakeRefKey(ctx, desc))
			return nil, nil
		})

		return client.Push(ctx, ref, target,
			containerd.WithResolver(resolver),
			containerd.WithImageHandler(jobHandler),
		)
	})

	eg.Go(func() error {
		var (
			ticker = time.NewTicker(100 * time.Millisecond)
			fw     = json.NewEncoder(w)
			done   bool
		)

		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				for _, st := range ongoing.status() {
					fw.Encode(st)
				}

				if done {
					return nil
				}
			case <-doneCh:
				done = true
			case <-ctx.Done():
				done = true // allow ui to update once more
			}
		}
	})

	return eg.Wait()
}

type pushjobs struct {
	jobs    map[string]struct{}
	ordered []string
	tracker docker.StatusTracker
	mu      sync.Mutex
}

func newPushJobs(tracker docker.StatusTracker) *pushjobs {
	return &pushjobs{
		jobs:    make(map[string]struct{}),
		tracker: tracker,
	}
}

func (j *pushjobs) add(ref string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if _, ok := j.jobs[ref]; ok {
		return
	}
	j.ordered = append(j.ordered, ref)
	j.jobs[ref] = struct{}{}
}

type progressDetail struct {
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
}

type imageProgess struct {
	Status         string         `json:"status"`
	ProgressDetail progressDetail `json:"progressDetail"`
	Progress       string         `json:"progress"`
	ID             string         `json:"id"`
}

func (j *pushjobs) status() []imageProgess {
	j.mu.Lock()
	defer j.mu.Unlock()

	statuses := make([]imageProgess, 0, len(j.jobs))
	for _, name := range j.ordered {
		si := imageProgess{
			ID: name,
		}

		status, err := j.tracker.GetStatus(name)
		if err != nil {
			si.Status = "waiting"
		} else {
			si.ProgressDetail.Total = status.Total
			si.ProgressDetail.Current = status.Offset
			if status.Offset >= status.Total {
				if status.UploadUUID == "" {
					si.Status = "done"
				} else {
					si.Status = "committing"
				}
			} else {
				si.Status = "uploading"
			}
		}
		statuses = append(statuses, si)
	}

	return statuses
}
