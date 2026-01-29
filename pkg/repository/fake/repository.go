// Copyright 2026 tsuru authors. All rights reserved.
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
}

func (f *FakeRepository) create(name string) error {
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

func (f *FakeRepository) exists(name string) (bool, error) {
	if f.RepoExists == nil {
		f.RepoExists = make(map[string]bool)
	}
	exists := f.RepoExists[name]
	return exists, nil
}

func (f *FakeRepository) Ensure(ctx context.Context, name string) error {
	exists, err := f.exists(name)
	if err != nil {
		return err
	}
	if !exists {
		return f.create(name)
	}
	return nil
}
