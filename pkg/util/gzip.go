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
)

func CompressGZIPFile(ctx context.Context, w io.Writer, rootDir string) error {
	if err := ctx.Err(); err != nil {
		return err
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

		f, err := os.Open(path)
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

		finfo := h.FileInfo()

		target := filepath.Join(dir, filepath.Clean(h.Name))

		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, finfo.Mode()); err != nil {
				return err
			}

		case tar.TypeReg:
			var f *os.File
			f, err = os.OpenFile(target, os.O_CREATE|os.O_RDWR, finfo.Mode())
			if err != nil {
				return err
			}

			if _, err = io.CopyN(f, tr, finfo.Size()); err != nil {
				return err
			}

			if err = f.Chown(h.Uid, h.Gid); err != nil {
				return err
			}

			if err = f.Close(); err != nil {
				return err
			}

			if err = os.Chtimes(target, h.ChangeTime, finfo.ModTime()); err != nil {
				return err
			}

		case tar.TypeSymlink:
			if err = os.Symlink(h.Linkname, target); err != nil {
				return err
			}

			if err = os.Lchown(target, h.Uid, h.Gid); err != nil {
				return err
			}

		default:
			return fmt.Errorf("not supported file type at file %q", h.Name)
		}
	}

	return nil
}
