// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressAndExtractGZIPFile(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "dir", "file.txt"), []byte("hello world"), 0o644))

	var data bytes.Buffer
	require.NoError(t, CompressGZIPFile(context.TODO(), &data, src))

	dst := t.TempDir()
	require.NoError(t, ExtractGZIPFileToDir(context.TODO(), &data, dst))

	got, err := os.ReadFile(filepath.Join(dst, "dir", "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(got))
}

func TestExtractGZIPFileToDir_TruncatedArchive(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "file.txt"), bytes.Repeat([]byte("a"), 4096), 0o644))

	var data bytes.Buffer
	require.NoError(t, CompressGZIPFile(context.TODO(), &data, src))

	truncated := data.Bytes()[:data.Len()/2]

	err := ExtractGZIPFileToDir(context.TODO(), bytes.NewReader(truncated), t.TempDir())
	assert.ErrorContains(t, err, "failed to read next file in the tarball")
}
