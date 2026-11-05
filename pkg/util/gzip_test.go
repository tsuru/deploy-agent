// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarEntry describes one entry of the archive under test. A nil uid/gid means
// "use the current process owner", which keeps the happy-path tests runnable
// as an unprivileged user.
type tarEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
	uid      *int
	gid      *int
	mode     int64
	modTime  time.Time
}

func intPtr(i int) *int { return &i }

// buildArchive builds a gzipped tar with the given entries. Entries with a
// zero typeflag are written as regular files.
func buildArchive(t *testing.T, entries []tarEntry) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)

	for _, e := range entries {
		typeflag := e.typeflag
		mode := e.mode
		if mode == 0 {
			mode = 0o644
			if typeflag == tar.TypeDir {
				mode = 0o755
			}
		}

		uid, gid := os.Getuid(), os.Getgid()
		if e.uid != nil {
			uid = *e.uid
		}
		if e.gid != nil {
			gid = *e.gid
		}

		h := &tar.Header{
			Name:     e.name,
			Linkname: e.linkname,
			Typeflag: typeflag,
			Mode:     mode,
			Uid:      uid,
			Gid:      gid,
			ModTime:  e.modTime,
		}

		if typeflag == tar.TypeSymlink || typeflag == tar.TypeDir {
			require.NoError(t, tw.WriteHeader(h))
			continue
		}

		h.Size = int64(len(e.body))
		require.NoError(t, tw.WriteHeader(h))
		_, err := tw.Write([]byte(e.body))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	return &buf
}

// extractToSandbox creates an isolated sandbox with a "context" directory, so
// tests can assert that nothing is ever written next to it, then extracts the
// archive built from entries into that directory.
func extractToSandbox(t *testing.T, entries []tarEntry) (sandbox, dst string, err error) {
	t.Helper()

	sandbox = t.TempDir()
	dst = filepath.Join(sandbox, "context")
	require.NoError(t, os.Mkdir(dst, 0o755))

	buf := buildArchive(t, entries)
	err = ExtractGZIPFileToDir(context.Background(), buf, dst)
	return sandbox, dst, err
}

// assertNothingEscaped fails if the sandbox (the parent of the extraction
// directory) contains anything but the extraction directory itself.
func assertNothingEscaped(t *testing.T, sandbox string) {
	t.Helper()

	entries, err := os.ReadDir(sandbox)
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, "context", e.Name(), "unexpected entry escaped the extraction directory")
	}
}

// listTree returns every path below root, relative to root, directories
// without a trailing slash.
func listTree(t *testing.T, root string) []string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	require.NoError(t, err)
	return out
}

// ---------------------------------------------------------------------------
// Happy path / output-directory behavior.
// ---------------------------------------------------------------------------

func TestExtractGZIPFileToDir_ExtractsFileContents(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "a.txt", body: "hello world\n"},
		{name: "b.txt", body: ""},
		{name: "c.bin", body: "\x00\x01\x02\xff"},
	})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", string(got))

	got, err = os.ReadFile(filepath.Join(dst, "b.txt"))
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = os.ReadFile(filepath.Join(dst, "c.bin"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0x00, 0x01, 0x02, 0xff}, got)

	assertNothingEscaped(t, sandbox)
}

func TestExtractGZIPFileToDir_CreatesNestedDirectories(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "a/", typeflag: tar.TypeDir},
		{name: "a/b/", typeflag: tar.TypeDir},
		{name: "a/b/c/", typeflag: tar.TypeDir},
		{name: "a/b/c/deep.txt", body: "deep"},
	})
	require.NoError(t, err)

	fi, err := os.Stat(filepath.Join(dst, "a", "b", "c"))
	require.NoError(t, err)
	assert.True(t, fi.IsDir())

	got, err := os.ReadFile(filepath.Join(dst, "a", "b", "c", "deep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "deep", string(got))
	assertNothingEscaped(t, sandbox)
}

func TestExtractGZIPFileToDir_PreservesPermissions(t *testing.T) {
	_, dst, err := extractToSandbox(t, []tarEntry{
		{name: "run.sh", body: "#!/bin/sh\n", mode: 0o755},
		{name: "read.txt", body: "x", mode: 0o600},
		{name: "locked/", typeflag: tar.TypeDir, mode: 0o700},
	})
	require.NoError(t, err)

	fi, err := os.Stat(filepath.Join(dst, "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())

	fi, err = os.Stat(filepath.Join(dst, "read.txt"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	fi, err = os.Stat(filepath.Join(dst, "locked"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
}

func TestExtractGZIPFileToDir_PreservesModificationTime(t *testing.T) {
	want := time.Unix(1700000000, 0)

	_, dst, err := extractToSandbox(t, []tarEntry{
		{name: "timed.txt", body: "x", modTime: want},
	})
	require.NoError(t, err)

	fi, err := os.Stat(filepath.Join(dst, "timed.txt"))
	require.NoError(t, err)
	assert.Equal(t, want.Unix(), fi.ModTime().Unix())
}

func TestExtractGZIPFileToDir_NormalizesSafePaths(t *testing.T) {
	// Paths that stay inside the destination but contain redundant or
	// self-referential components must be normalized, not rejected.
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "sub/", typeflag: tar.TypeDir},
		{name: "double/", typeflag: tar.TypeDir},
		{name: "./dot-prefixed.txt", body: "a"},
		{name: "double//slash.txt", body: "b"},
		{name: "sub/../stays-inside.txt", body: "c"},
		{name: "sub/./also-inside.txt", body: "d"},
	})
	require.NoError(t, err)

	for name, want := range map[string]string{
		"dot-prefixed.txt":    "a",
		"double/slash.txt":    "b",
		"stays-inside.txt":    "c",
		"sub/also-inside.txt": "d",
	} {
		got, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(name)))
		require.NoErrorf(t, err, "reading %s", name)
		assert.Equal(t, want, string(got), "content of %s", name)
	}
	assertNothingEscaped(t, sandbox)
}

func TestExtractGZIPFileToDir_OutputTree(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "app/", typeflag: tar.TypeDir},
		{name: "app/main.go", body: "package main\n"},
		{name: "app/vendor/", typeflag: tar.TypeDir},
		{name: "app/vendor/lib.go", body: "package vendor\n"},
		{name: "app/real.txt", body: "real"},
		{name: "app/link", typeflag: tar.TypeSymlink, linkname: "real.txt"},
		{name: "README.md", body: "readme"},
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{
		"app",
		"app/main.go",
		"app/vendor",
		"app/vendor/lib.go",
		"app/real.txt",
		"app/link",
		"README.md",
	}, listTree(t, dst))
	assertNothingEscaped(t, sandbox)
}

func TestExtractGZIPFileToDir_AllowsSymlinksInsideDestination(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		read    string
		want    string
	}{
		{
			name: "sibling relative link",
			entries: []tarEntry{
				{name: "real.txt", body: "hi"},
				{name: "link", typeflag: tar.TypeSymlink, linkname: "real.txt"},
			},
			read: "link",
			want: "hi",
		},
		{
			name: "relative link walking up within the destination",
			entries: []tarEntry{
				{name: "sub/", typeflag: tar.TypeDir},
				{name: "a.txt", body: "aaa"},
				{name: "sub/link", typeflag: tar.TypeSymlink, linkname: "../a.txt"},
			},
			read: "sub/link",
			want: "aaa",
		},
		{
			name: "chain of links",
			entries: []tarEntry{
				{name: "a.txt", body: "aaa"},
				{name: "link1", typeflag: tar.TypeSymlink, linkname: "a.txt"},
				{name: "link2", typeflag: tar.TypeSymlink, linkname: "link1"},
			},
			read: "link2",
			want: "aaa",
		},
		{
			name: "link to a directory",
			entries: []tarEntry{
				{name: "dir/", typeflag: tar.TypeDir},
				{name: "dir/f.txt", body: "f"},
				{name: "dirlink", typeflag: tar.TypeSymlink, linkname: "dir"},
			},
			read: "dirlink/f.txt",
			want: "f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox, dst, err := extractToSandbox(t, tt.entries)
			require.NoError(t, err)

			got, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(tt.read)))
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
			assertNothingEscaped(t, sandbox)
		})
	}
}

func TestExtractGZIPFileToDir_EmptyArchive(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, nil)
	require.NoError(t, err)
	assert.Empty(t, listTree(t, dst))
	assertNothingEscaped(t, sandbox)
}

func TestExtractGZIPFileToDir_ExtractsIntoExistingDirectory(t *testing.T) {
	sandbox := t.TempDir()
	dst := filepath.Join(sandbox, "context")
	require.NoError(t, os.Mkdir(dst, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "pre-existing.txt"), []byte("old"), 0o644))

	buf := buildArchive(t, []tarEntry{
		{name: "new.txt", body: "new"},
	})
	require.NoError(t, ExtractGZIPFileToDir(context.Background(), buf, dst))

	got, err := os.ReadFile(filepath.Join(dst, "new.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	// Pre-existing content must be left alone.
	got, err = os.ReadFile(filepath.Join(dst, "pre-existing.txt"))
	require.NoError(t, err)
	assert.Equal(t, "old", string(got))
	assertNothingEscaped(t, sandbox)
}
