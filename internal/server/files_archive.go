package server

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const maxFileTransfer = 512 << 20
const maxArchiveEntries = 20000

var errArchiveLimit = errors.New("archive exceeds the file count or size limit")

func regularFile(h *tar.Header) bool {
	return h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA
}

func archivePath(name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", errFilePath
	}
	if _, err := filePath("/" + name); err != nil {
		return "", err
	}
	return name, nil
}

type fileArchiveSink func(*tar.Header, io.Reader) error

func writeFileArchive(out io.Writer, format string, entries func(fileArchiveSink) error) error {
	switch format {
	case "zip":
		z := zip.NewWriter(out)
		err := entries(func(h *tar.Header, src io.Reader) error {
			f := &zip.FileHeader{Name: h.Name, Modified: h.ModTime, Method: zip.Deflate}
			f.SetMode(h.FileInfo().Mode().Perm())
			if h.Typeflag == tar.TypeDir {
				f.Name += "/"
				f.SetMode(os.ModeDir | h.FileInfo().Mode().Perm())
			}
			w, err := z.CreateHeader(f)
			if err == nil {
				_, err = io.Copy(w, src)
			}
			return err
		})
		return errors.Join(err, z.Close())
	case "tar.gz":
		g := gzip.NewWriter(out)
		t := tar.NewWriter(g)
		err := entries(func(h *tar.Header, src io.Reader) error {
			if err := t.WriteHeader(h); err != nil {
				return err
			}
			_, err := io.Copy(t, src)
			return err
		})
		return errors.Join(err, t.Close(), g.Close())
	default:
		return errors.New("unsupported archive format")
	}
}

func normalizeFileArchive(in *os.File, name string, out io.Writer, check func(string) error) error {
	tw := tar.NewWriter(out)
	seen := map[string]bool{}
	var size int64
	add := func(h *tar.Header, src io.Reader) error {
		n, err := archivePath(h.Name)
		if err != nil || (!regularFile(h) && h.Typeflag != tar.TypeDir) {
			return errors.New("archive contains an unsafe path or unsupported file type")
		}
		if _, exists := seen[n]; exists {
			return errors.New("archive contains duplicate paths")
		}
		if h.Size < 0 || h.Size > maxFileTransfer-size || len(seen) >= maxArchiveEntries {
			return errArchiveLimit
		}
		if err := check(n); err != nil {
			return err
		}
		seen[n] = h.Typeflag == tar.TypeDir
		size += h.Size
		if err := tw.WriteHeader(&tar.Header{
			Name: n, Typeflag: h.Typeflag, Mode: h.Mode & 0777, Size: h.Size, ModTime: h.ModTime,
		}); err != nil {
			return err
		}
		_, err = io.CopyN(tw, src, h.Size)
		return err
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		st, err := in.Stat()
		if err != nil {
			return err
		}
		z, err := zip.NewReader(in, st.Size())
		if err != nil {
			return err
		}
		for _, f := range z.File {
			mode := f.Mode()
			h := &tar.Header{Name: f.Name, Mode: int64(mode.Perm()), Size: int64(f.UncompressedSize64), ModTime: f.Modified, Typeflag: tar.TypeReg}
			switch {
			case mode.IsDir():
				h.Typeflag = tar.TypeDir
			case !mode.IsRegular():
				return errors.New("archive contains an unsupported file type")
			}
			r, err := f.Open()
			if err != nil {
				return err
			}
			err = add(h, r)
			if err == nil {
				var b [1]byte
				_, err = r.Read(b[:])
				if err == io.EOF {
					err = nil
				}
			}
			r.Close()
			if err != nil {
				return err
			}
		}
	case strings.HasSuffix(lower, ".tar"), strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		var r io.Reader = in
		if !strings.HasSuffix(lower, ".tar") {
			g, err := gzip.NewReader(in)
			if err != nil {
				return err
			}
			defer g.Close()
			r = g
		}
		t := tar.NewReader(r)
		for {
			h, err := t.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if err := add(h, t); err != nil {
				return err
			}
		}
	default:
		return errors.New("unsupported archive format")
	}
	for n := range seen {
		for parent := path.Dir(n); parent != "."; parent = path.Dir(parent) {
			if dir, ok := seen[parent]; ok && !dir {
				return errors.New("archive contains conflicting paths")
			}
		}
	}
	return tw.Close()
}

func (fs fileDocker) archive(ctx context.Context, out io.Writer, format string, names []string) error {
	roots := map[string]bool{}
	var size int64
	var count int
	for _, name := range names {
		if _, err := filePath(name); err != nil || name == "" || roots[path.Base(name)] {
			return errFilePath
		}
		roots[path.Base(name)] = true
		if _, err := fs.check(ctx, name); err != nil {
			return err
		}
	}
	return writeFileArchive(out, format, func(sink fileArchiveSink) error {
		for _, name := range names {
			err := fs.read(ctx, name, func(t *tar.Reader) error {
				root := ""
				for first := true; ; first = false {
					h, err := t.Next()
					if err == io.EOF {
						return nil
					}
					if err != nil {
						return err
					}
					n := strings.TrimSuffix(h.Name, "/")
					if first {
						root = n
					}
					var entry string
					if n == root {
						if name == "/" {
							continue
						}
						entry = path.Base(name)
					} else if rel, found := strings.CutPrefix(n, root+"/"); found {
						entry = rel
						if name != "/" {
							entry = path.Base(name) + "/" + entry
						}
					} else {
						return errFilePath
					}
					if !regularFile(h) && h.Typeflag != tar.TypeDir {
						continue
					}
					entry, err = archivePath(entry)
					if err != nil {
						return err
					}
					count++
					if count > maxArchiveEntries || h.Size > maxFileTransfer-size {
						return errArchiveLimit
					}
					size += h.Size
					if err := sink(&tar.Header{Name: entry, Typeflag: h.Typeflag, Mode: h.Mode & 0777, Size: h.Size, ModTime: h.ModTime}, t); err != nil {
						return err
					}
				}
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (fs fileDocker) downloadArchive(ctx context.Context, w http.ResponseWriter, name, format string) error {
	f, cleanup, err := stageFile()
	defer cleanup()
	if err != nil {
		return err
	}
	if err := fs.archive(ctx, f, format, []string{name}); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	filename := path.Base(name)
	if name == "/" {
		filename = "files"
	}
	fileAttachment(w, filename+"."+format, "application/octet-stream")
	_, _ = io.Copy(w, f)
	return nil
}

func (fs fileDocker) compress(ctx context.Context, name string, action fileAction) error {
	if len(action.Paths) == 0 || len(action.Paths) > 100 || !strings.HasSuffix(name, "."+action.Format) {
		return errors.New("select files and choose a matching archive extension")
	}
	if err := fs.destination(ctx, name, false); err != nil {
		return err
	}
	f, cleanup, err := stageFile()
	defer cleanup()
	if err != nil {
		return err
	}
	if err := fs.archive(ctx, f, action.Format, action.Paths); err != nil {
		return err
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return fs.put(ctx, name, &tar.Header{Typeflag: tar.TypeReg, Size: size, Mode: 0644, ModTime: time.Now()}, f)
}

func (fs fileDocker) extract(ctx context.Context, name, dir string) error {
	if dir == "" {
		dir = path.Dir(name)
	}
	if _, err := filePath(dir); err != nil {
		return err
	}
	h, err := fs.check(ctx, dir)
	if err != nil {
		return err
	}
	if h.Typeflag != tar.TypeDir {
		return errFilePath
	}
	if _, err := fs.check(ctx, name); err != nil {
		return err
	}
	in, cleanIn, err := stageFile()
	defer cleanIn()
	if err != nil {
		return err
	}
	if err := fs.withFile(ctx, name, func(h *tar.Header, src io.Reader) error {
		if h.Size > maxFileTransfer {
			return errArchiveLimit
		}
		_, err := io.Copy(in, src)
		return err
	}); err != nil {
		return err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	out, cleanOut, err := stageFile()
	defer cleanOut()
	if err != nil {
		return err
	}
	checked := map[string]bool{}
	if err := normalizeFileArchive(in, name, out, func(n string) error {
		top, _, _ := strings.Cut(n, "/")
		if checked[top] {
			return nil
		}
		dst := path.Join(dir, top)
		if err := fs.destination(ctx, dst, false); err != nil {
			return err
		}
		checked[top] = true
		return nil
	}); err != nil {
		return err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return fs.writeTar(ctx, dir, out)
}
