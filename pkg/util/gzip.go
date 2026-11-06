// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func CompressGZIPFile(ctx context.Context, w io.Writer, rootDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Canonicalize the root so relative names stay correct when it (or a
	// parent) is a symlink, e.g. on macOS where /var -> /private/var.
	if resolved, err := filepath.EvalSymlinks(rootDir); err == nil {
		rootDir = resolved
	}

	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	defer zw.Close()

	tw := tar.NewWriter(zw)
	defer tw.Close()

	return filepath.WalkDir(rootDir, fs.WalkDirFunc(func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if err = ctx.Err(); err != nil {
			return err
		}

		var link string
		if d.Type()&fs.ModeSymlink != 0 {
			var symlink string
			symlink, err = filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}

			link, err = filepath.Rel(rootDir, symlink)
			if err != nil {
				return err
			}
		}

		if !d.Type().IsDir() && !d.Type().IsRegular() && d.Type()&fs.ModeSymlink == 0 {
			return fmt.Errorf("unsupported file type: %s %s", d.Type(), path)
		}

		relpath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		if relpath == "." { // root dir, should skit its creation
			return nil
		}

		finfo, err := d.Info()
		if err != nil {
			return err
		}

		var h *tar.Header
		h, err = tar.FileInfoHeader(finfo, link)
		if err != nil {
			return err
		}

		h.Name = relpath
		if d.IsDir() {
			h.Name += "/"
		}

		if err = tw.WriteHeader(h); err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := os.Open(path) //nolint:gosec // walks the agent-generated source directory
		if err != nil {
			return err
		}

		if _, err = io.CopyN(tw, f, h.Size); err != nil {
			return err
		}

		if err = f.Close(); err != nil {
			return err
		}

		return nil
	}))
}

func ExtractGZIPFileToDir(ctx context.Context, r io.Reader, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	// Canonicalize the destination so comparisons against symlink-resolved
	// paths are consistent (on macOS /var is a symlink to /private/var).
	if resolved, rerr := filepath.EvalSymlinks(dir); rerr == nil {
		dir = resolved
	}

	// os.Root confines every subsequent operation to dir: any name whose
	// components escape the root is rejected by the OS layer itself.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	zr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}

	tr := tar.NewReader(zr)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		finfo := h.FileInfo()

		name, err := archiveName(h.Name)
		if err != nil {
			return err
		}

		switch h.Typeflag {
		case tar.TypeDir:
			if err = root.MkdirAll(name, finfo.Mode().Perm()); err != nil {
				return err
			}

		case tar.TypeReg:
			var f *os.File
			f, err = root.OpenFile(name, os.O_CREATE|os.O_RDWR|os.O_TRUNC, finfo.Mode().Perm())
			if err != nil {
				return err
			}

			if _, err = io.CopyN(f, tr, finfo.Size()); err != nil {
				f.Close()
				return err
			}

			// NOTE: intentionally not chowning to the header's uid/gid: the
			// archive is attacker-controlled and the agent may run as root.

			if err = f.Close(); err != nil {
				return err
			}

			if err = root.Chtimes(name, h.ChangeTime, finfo.ModTime()); err != nil {
				return err
			}

		case tar.TypeSymlink:
			if err = rejectSymlinkedParents(root, name); err != nil {
				return err
			}

			if err = validateSymlinkTarget(name, h.Linkname); err != nil {
				return err
			}

			// Root.Symlink creates the link within the confined root and does
			// not follow a symlinked parent outside it. Note that it does not
			// validate oldname, which is why validateSymlinkTarget above is
			// still required.
			if err = root.Symlink(h.Linkname, name); err != nil {
				return err
			}

			// NOTE: intentionally not lchowning to the header's uid/gid.

		default:
			return fmt.Errorf("not supported file type at file %q", h.Name)
		}
	}

	return nil
}

// archiveName normalizes an archive entry name and rejects absolute paths and
// names that escape upwards. Containment is additionally enforced by os.Root
// at the point of each filesystem operation; this check fails loudly and
// rejects hostile names before touching the filesystem.
func archiveName(name string) (string, error) {
	if name == "" {
		return "", errors.New("illegal empty path in archive")
	}

	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) ||
		clean == ".." ||
		strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal path in archive: %q", name)
	}

	return clean, nil
}

// validateSymlinkTarget rejects symlinks whose destination leaves the
// extraction root. Absolute targets are rejected; relative targets are
// resolved against the link's own directory and must stay inside the root.
func validateSymlinkTarget(name, linkname string) error {
	if filepath.IsAbs(linkname) {
		return fmt.Errorf("illegal absolute symlink target %q for entry %q", linkname, name)
	}

	resolved := filepath.Join(filepath.Dir(name), linkname)
	if resolved == ".." || strings.HasPrefix(resolved, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("illegal symlink target %q for entry %q", linkname, name)
	}

	return nil
}

// rejectSymlinkedParents makes validateSymlinkTarget's lexical check exact: if
// no parent component is a symlink, the link's real directory is its lexical
// one. Otherwise a link such as "a/a/a/leak" could be created through a
// symlinked parent and resolve somewhere else than the lexical path suggests.
func rejectSymlinkedParents(root *os.Root, name string) error {
	parent := filepath.Dir(name)
	if parent == "." {
		return nil
	}

	var cur string
	for _, part := range strings.Split(filepath.ToSlash(parent), "/") {
		cur = filepath.Join(cur, part)

		fi, err := root.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("illegal symlink entry %q: parent %q is a symlink", name, cur)
		}
	}

	return nil
}
