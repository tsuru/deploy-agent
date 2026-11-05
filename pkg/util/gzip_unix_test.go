// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package util

import (
	"bytes"
	"context"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompressGZIPFileRejectsUnsupportedType(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644))

	var buf bytes.Buffer
	err := CompressGZIPFile(context.Background(), &buf, src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported file type")
}
