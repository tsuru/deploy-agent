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
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarEntry describes one entry of the archive under test. A nil uid/gid means
// "use the current process owner", which keeps the happy-path tests runnable
// as an unprivileged user.
type tarEntry struct {
	modTime  time.Time
	uid      *int
	gid      *int
	name     string
	body     string
	linkname string
	mode     int64
	typeflag byte
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
		read    string
		want    string
		entries []tarEntry
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
	require.NoError(t, os.WriteFile(filepath.Join(dst, "pre-existing.txt"), []byte("old"), 0o600))

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

// ---------------------------------------------------------------------------
// Vulnerability: path traversal (zip slip).
// ---------------------------------------------------------------------------

func TestExtractGZIPFileToDir_RejectsPathTraversal(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
	}{
		{
			name:    "parent traversal regular file",
			entries: []tarEntry{{name: "../escape.txt", body: "pwned"}},
		},
		{
			name:    "double parent traversal regular file",
			entries: []tarEntry{{name: "../../escape.txt", body: "pwned"}},
		},
		{
			name:    "nested traversal regular file",
			entries: []tarEntry{{name: "a/b/../../../escape.txt", body: "pwned"}},
		},
		{
			name:    "absolute path regular file",
			entries: []tarEntry{{name: "/escape.txt", body: "pwned"}},
		},
		{
			name:    "parent traversal directory",
			entries: []tarEntry{{name: "../escapedir/", typeflag: tar.TypeDir}},
		},
		{
			name:    "absolute path directory",
			entries: []tarEntry{{name: "/escapedir/", typeflag: tar.TypeDir}},
		},
		{
			name:    "traversal via safe prefix then escape",
			entries: []tarEntry{{name: "safe/../../escape.txt", body: "pwned"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox, _, err := extractToSandbox(t, tt.entries)
			require.Error(t, err, "extraction must reject a path escaping the destination")
			assertNothingEscaped(t, sandbox)
		})
	}
}

// ---------------------------------------------------------------------------
// Vulnerability: symlink escape.
// ---------------------------------------------------------------------------

func TestExtractGZIPFileToDir_RejectsSymlinkEscape(t *testing.T) {
	tests := []struct {
		name    string
		created string
		entries []tarEntry
	}{
		{
			name:    "absolute root",
			entries: []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: "/"}},
			created: "slink",
		},
		{
			name:    "absolute etc",
			entries: []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: "/etc"}},
			created: "slink",
		},
		{
			name:    "absolute home",
			entries: []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: "/root"}},
			created: "slink",
		},
		{
			name:    "relative escaping with one level",
			entries: []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: "../escape"}},
			created: "slink",
		},
		{
			name:    "relative escaping with many levels",
			entries: []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: "../../../../etc"}},
			created: "slink",
		},
		{
			name:    "relative to parent directory",
			entries: []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: ".."}},
			created: "slink",
		},
		{
			name: "nested entry escaping",
			entries: []tarEntry{
				{name: "sub/", typeflag: tar.TypeDir},
				{name: "sub/slink", typeflag: tar.TypeSymlink, linkname: "../../../etc"},
			},
			created: "sub/slink",
		},
		{
			name: "escape via symlinked parent pointing to root",
			entries: []tarEntry{
				{name: "a", typeflag: tar.TypeSymlink, linkname: "."},
				{name: "a/a/a/leak", typeflag: tar.TypeSymlink, linkname: "../../../escape"},
			},
			created: "leak",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox, dst, err := extractToSandbox(t, tt.entries)
			require.Error(t, err, "extraction must reject a symlink pointing outside the destination")
			assertNothingEscaped(t, sandbox)

			// The symlink itself must not have been created.
			_, statErr := os.Lstat(filepath.Join(dst, filepath.FromSlash(tt.created)))
			assert.True(t, os.IsNotExist(statErr), "escaping symlink must not be created")
		})
	}
}

func TestExtractGZIPFileToDir_RejectsWriteThroughSymlink(t *testing.T) {
	outside := t.TempDir()

	sandbox, _, err := extractToSandbox(t, []tarEntry{
		{name: "slink", typeflag: tar.TypeSymlink, linkname: outside},
		{name: "slink/pwned.txt", body: "pwned"},
	})
	require.Error(t, err, "extraction must reject writing through an escaping symlink")
	assertNothingEscaped(t, sandbox)

	entries, rerr := os.ReadDir(outside)
	require.NoError(t, rerr)
	assert.Empty(t, entries, "archive wrote through a symlink outside the destination")
}

func TestExtractGZIPFileToDir_RejectsPreexistingSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	sandbox := t.TempDir()
	dst := filepath.Join(sandbox, "context")
	require.NoError(t, os.Mkdir(dst, 0o755))

	// Plant a symlink inside the destination pointing outside of it, as if the
	// destination were not pristine.
	require.NoError(t, os.Symlink(outside, filepath.Join(dst, "link")))

	buf := buildArchive(t, []tarEntry{
		{name: "link/", typeflag: tar.TypeDir},
	})
	err := ExtractGZIPFileToDir(context.Background(), buf, dst)
	require.Error(t, err, "extraction must not traverse a pre-existing escaping symlink")

	entries, rerr := os.ReadDir(outside)
	require.NoError(t, rerr)
	assert.Empty(t, entries)
}

// ---------------------------------------------------------------------------
// Vulnerability: ownership from the archive must be ignored.
// ---------------------------------------------------------------------------

func TestExtractGZIPFileToDir_DoesNotApplyOwnerFromArchive(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "owned.txt", body: "x", uid: intPtr(12345), gid: intPtr(54321)},
	})
	require.NoError(t, err, "extraction must not fail on a foreign uid/gid")
	assertNothingEscaped(t, sandbox)

	fi, err := os.Stat(filepath.Join(dst, "owned.txt"))
	require.NoError(t, err)
	st, ok := fi.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.EqualValues(t, os.Getuid(), st.Uid, "file must keep the current owner")
	assert.EqualValues(t, os.Getgid(), st.Gid, "file must keep the current group")
}

func TestExtractGZIPFileToDir_DoesNotLchownSymlinkFromArchive(t *testing.T) {
	_, dst, err := extractToSandbox(t, []tarEntry{
		{name: "a.txt", body: "aaa"},
		{name: "link", typeflag: tar.TypeSymlink, linkname: "a.txt", uid: intPtr(12345), gid: intPtr(54321)},
	})
	require.NoError(t, err, "extraction must not fail on a foreign uid/gid for symlinks")

	fi, err := os.Lstat(filepath.Join(dst, "link"))
	require.NoError(t, err)
	st, ok := fi.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.EqualValues(t, os.Getuid(), st.Uid)
	assert.EqualValues(t, os.Getgid(), st.Gid)
}

// ---------------------------------------------------------------------------
// Error handling.
// ---------------------------------------------------------------------------

func TestExtractGZIPFileToDir_Errors(t *testing.T) {
	t.Run("returns context error when canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		buf := buildArchive(t, []tarEntry{{name: "a.txt", body: "a"}})
		err := ExtractGZIPFileToDir(ctx, buf, t.TempDir())
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("rejects invalid gzip data", func(t *testing.T) {
		err := ExtractGZIPFileToDir(context.Background(), bytes.NewReader([]byte("not gzip")), t.TempDir())
		require.Error(t, err)
	})

	t.Run("rejects corrupt tar data without panicking", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ExtractGZIPFileToDir panicked on corrupt tar data: %v", r)
			}
		}()

		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, err := zw.Write([]byte("this is not a valid tar stream, just garbage bytes"))
		require.NoError(t, err)
		require.NoError(t, zw.Close())

		err = ExtractGZIPFileToDir(context.Background(), &buf, t.TempDir())
		require.Error(t, err)
	})

	t.Run("rejects unsupported entry types", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			typeflag byte
		}{
			{"hardlink", tar.TypeLink},
			{"fifo", tar.TypeFifo},
			{"char device", tar.TypeChar},
			{"block device", tar.TypeBlock},
		} {
			t.Run(tt.name, func(t *testing.T) {
				sandbox, _, err := extractToSandbox(t, []tarEntry{
					{name: "weird", typeflag: tt.typeflag, linkname: "target"},
				})
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not supported file type")
				assertNothingEscaped(t, sandbox)
			})
		}
	})
}

// TestExtractGZIPFileToDir_StopsAtFirstEscape documents that extraction is
// streaming: entries before an illegal one are already written, while the
// illegal entry itself is rejected and nothing escapes.
func TestExtractGZIPFileToDir_StopsAtFirstEscape(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "ok.txt", body: "fine"},
		{name: "../escape.txt", body: "pwned"},
		{name: "never.txt", body: "never"},
	})
	require.Error(t, err)
	assertNothingEscaped(t, sandbox)

	got, rerr := os.ReadFile(filepath.Join(dst, "ok.txt"))
	require.NoError(t, rerr)
	assert.Equal(t, "fine", string(got))

	_, rerr = os.Stat(filepath.Join(dst, "never.txt"))
	assert.True(t, os.IsNotExist(rerr), "entries after the illegal one must not be extracted")
}

// ---------------------------------------------------------------------------
// Regression tests for the os.Root migration.
// ---------------------------------------------------------------------------

// TestExtractGZIPFileToDir_DirectoryEntriesAcceptTypeBits guards the bug where
// finfo.Mode() includes fs.ModeDir for directory entries; os.Root.Mkdir rejects
// mode bits outside 0o777, so the type bits must be masked before creation.
func TestExtractGZIPFileToDir_DirectoryEntriesAcceptTypeBits(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "d700/", typeflag: tar.TypeDir, mode: 0o700},
		{name: "d750/", typeflag: tar.TypeDir, mode: 0o750},
		{name: "d700/f.txt", body: "x"},
	})
	require.NoError(t, err)
	assertNothingEscaped(t, sandbox)

	for name, want := range map[string]os.FileMode{
		"d700": 0o700,
		"d750": 0o750,
	} {
		fi, serr := os.Stat(filepath.Join(dst, name))
		require.NoError(t, serr)
		assert.True(t, fi.IsDir(), "%s must be a directory", name)
		assert.Equal(t, want, fi.Mode().Perm(), "permissions of %s", name)
	}
}

// TestExtractGZIPFileToDir_RelativeSymlinkTargetMath guards the bug where a
// relative link target was resolved against the filesystem root instead of the
// link's own directory, letting "../x" slip past validation.
func TestExtractGZIPFileToDir_RelativeSymlinkTargetMath(t *testing.T) {
	t.Run("safe targets stay inside", func(t *testing.T) {
		sandbox, dst, err := extractToSandbox(t, []tarEntry{
			{name: "a.txt", body: "aaa"},
			{name: "sub/", typeflag: tar.TypeDir},
			{name: "sub/deep/", typeflag: tar.TypeDir},
			{name: "sub/up", typeflag: tar.TypeSymlink, linkname: "../a.txt"},
			{name: "sub/deep/up", typeflag: tar.TypeSymlink, linkname: "../../a.txt"},
			{name: "sub/toroot", typeflag: tar.TypeSymlink, linkname: ".."},
		})
		require.NoError(t, err)
		assertNothingEscaped(t, sandbox)

		dstReal, rerr := filepath.EvalSymlinks(dst)
		require.NoError(t, rerr)

		for path, want := range map[string]string{
			"sub/up":      "aaa",
			"sub/deep/up": "aaa",
		} {
			got, readErr := os.ReadFile(filepath.Join(dst, filepath.FromSlash(path)))
			require.NoErrorf(t, readErr, "reading %s", path)
			assert.Equal(t, want, string(got))
		}

		fi, serr := os.Stat(filepath.Join(dst, "sub", "toroot"))
		require.NoError(t, serr)
		assert.True(t, fi.IsDir(), "link to root via .. must resolve inside")

		real, rerr := filepath.EvalSymlinks(filepath.Join(dst, "sub", "toroot"))
		require.NoError(t, rerr)
		assert.Equal(t, dstReal, real)
	})

	t.Run("escaping targets are rejected", func(t *testing.T) {
		tests := []struct {
			name     string
			linkPath string
			entries  []tarEntry
		}{
			{
				name:     "root link to parent",
				entries:  []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: ".."}},
				linkPath: "slink",
			},
			{
				name:     "root link to sibling escape",
				entries:  []tarEntry{{name: "slink", typeflag: tar.TypeSymlink, linkname: "../escape"}},
				linkPath: "slink",
			},
			{
				name: "nested link beyond root",
				entries: []tarEntry{
					{name: "sub/", typeflag: tar.TypeDir},
					{name: "sub/slink", typeflag: tar.TypeSymlink, linkname: "../../../etc"},
				},
				linkPath: "sub/slink",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sandbox, dst, err := extractToSandbox(t, tt.entries)
				require.Error(t, err)
				assertNothingEscaped(t, sandbox)

				_, statErr := os.Lstat(filepath.Join(dst, filepath.FromSlash(tt.linkPath)))
				assert.True(t, os.IsNotExist(statErr), "escaping symlink must not be created")
			})
		}
	})
}

// TestExtractGZIPFileToDir_CreatesDeepDirectoryEntriesWithoutParents covers the
// os.Root limitation that Root.Mkdir is not recursive: mkdirAll must create
// missing parents itself, even when the archive only carries a deep dir entry.
func TestExtractGZIPFileToDir_CreatesDeepDirectoryEntriesWithoutParents(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "a/b/c/", typeflag: tar.TypeDir, mode: 0o750},
	})
	require.NoError(t, err)
	assertNothingEscaped(t, sandbox)

	fi, err := os.Stat(filepath.Join(dst, "a", "b", "c"))
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, os.FileMode(0o750), fi.Mode().Perm())
}

// TestExtractGZIPFileToDir_SymlinkInNestedDirectory covers the os.Root
// limitation that Root has no Symlink method: creation goes through os.Symlink
// after the parent is validated with Root.OpenRoot. It must work inside nested
// directories and still reject escapes.
func TestExtractGZIPFileToDir_SymlinkInNestedDirectory(t *testing.T) {
	t.Run("safe nested link", func(t *testing.T) {
		sandbox, dst, err := extractToSandbox(t, []tarEntry{
			{name: "a.txt", body: "aaa"},
			{name: "sub/", typeflag: tar.TypeDir},
			{name: "sub/deep/", typeflag: tar.TypeDir},
			{name: "sub/deep/link", typeflag: tar.TypeSymlink, linkname: "../../a.txt"},
		})
		require.NoError(t, err)
		assertNothingEscaped(t, sandbox)

		link := filepath.Join(dst, "sub", "deep", "link")
		fi, serr := os.Lstat(link)
		require.NoError(t, serr)
		assert.NotZero(t, fi.Mode()&os.ModeSymlink)

		target, serr := os.Readlink(link)
		require.NoError(t, serr)
		assert.Equal(t, "../../a.txt", target)

		got, rerr := os.ReadFile(link)
		require.NoError(t, rerr)
		assert.Equal(t, "aaa", string(got))
	})

	t.Run("escaping nested link", func(t *testing.T) {
		sandbox, dst, err := extractToSandbox(t, []tarEntry{
			{name: "sub/", typeflag: tar.TypeDir},
			{name: "sub/deep/", typeflag: tar.TypeDir},
			{name: "sub/deep/link", typeflag: tar.TypeSymlink, linkname: "../../../escape"},
		})
		require.Error(t, err)
		assertNothingEscaped(t, sandbox)

		_, statErr := os.Lstat(filepath.Join(dst, "sub", "deep", "link"))
		assert.True(t, os.IsNotExist(statErr), "escaping nested symlink must not be created")
	})
}

// TestExtractGZIPFileToDir_PreservesModificationTimeInNestedDirectory covers the
// os.Root limitation that Root has no Chtimes method: times are applied through
// os.Chtimes on a path derived from the confined root, including for nested
// files whose parent directories were resolved through Root.
func TestExtractGZIPFileToDir_PreservesModificationTimeInNestedDirectory(t *testing.T) {
	want := time.Unix(1700001234, 0)

	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "sub/", typeflag: tar.TypeDir},
		{name: "sub/deep/", typeflag: tar.TypeDir},
		{name: "sub/deep/timed.txt", body: "x", modTime: want},
	})
	require.NoError(t, err)
	assertNothingEscaped(t, sandbox)

	fi, err := os.Stat(filepath.Join(dst, "sub", "deep", "timed.txt"))
	require.NoError(t, err)
	assert.Equal(t, want.Unix(), fi.ModTime().Unix())
}

// TestExtractGZIPFileToDir_TruncatesReusedPaths covers the O_TRUNC behaviour
// used with Root.OpenFile: a later, shorter entry for the same path must not
// leave bytes from the earlier entry behind.
func TestExtractGZIPFileToDir_TruncatesReusedPaths(t *testing.T) {
	sandbox, dst, err := extractToSandbox(t, []tarEntry{
		{name: "dup.txt", body: "this-is-long"},
		{name: "dup.txt", body: "short"},
	})
	require.NoError(t, err)
	assertNothingEscaped(t, sandbox)

	got, err := os.ReadFile(filepath.Join(dst, "dup.txt"))
	require.NoError(t, err)
	assert.Equal(t, "short", string(got))
}

// ---------------------------------------------------------------------------
// Contract with the BuildKit consumer lives in the integration suite:
// pkg/build/buildkit/build_test.go exercises the real
// generateBuildLocalDir -> ExtractGZIPFileToDir -> BuildKit COPY path.
// ---------------------------------------------------------------------------
