// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"context"
	"errors"
)

type FakeRepository struct {
	CreatedRepos map[string]bool
	RepoExists   map[string]bool
	AuthSuccess  bool
}

func (f *FakeRepository) Auth(ctx context.Context) error {
	if f.AuthSuccess {
		return nil
	}
	return errors.New("auth repository failed")
}

func (f *FakeRepository) Create(ctx context.Context, name string) error {
	if _, exists := f.RepoExists[name]; exists {
		return errors.New("repository already exists")
	}
	if f.CreatedRepos == nil {
		f.CreatedRepos = make(map[string]bool)
	}
	if f.RepoExists == nil {
		f.RepoExists = make(map[string]bool)
	}
	f.CreatedRepos[name] = true
	f.RepoExists[name] = true
	return nil
}

func (f *FakeRepository) Exists(ctx context.Context, name string) (bool, error) {
	if f.RepoExists == nil {
		f.RepoExists = make(map[string]bool)
	}
	exists := f.RepoExists[name]
	return exists, nil
}
