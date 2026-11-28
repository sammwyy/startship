package server

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const maxFileText = 5 << 20

func (a *Server) resolveFileTarget(p []string) (fileTarget, string, bool) {
	var t fileTarget
	if len(p) == 5 && p[3] == "files" {
		switch p[1] {
		case "volumes":
			v, err := getResource[Volume](a, "volumes", p[2])
			if err == nil && v.Purpose != backupVolumePurpose {
				t = fileTarget{volume: volumeName(v.ID), workspace: v.WorkspaceID, project: v.ProjectID}
			}
		case "containers":
			c, err := a.store.Get(p[2])
			if err == nil && c.Kind == "apps" {
				project, err := a.store.Get(c.ProjectID)
				if err == nil && project.Kind == "projects" {
					t.workspace, t.project = projectWorkspace(project), project.ID
					if d, err := a.store.Get(c.Active); err == nil && d.Kind == "deployments" && d.AppID == c.ID {
						t.container = d.Container
					}
				}
			}
		default:
			return t, "", false
		}
		return t, p[4], true
	}
	if len(p) == 9 && p[1] == "workspaces" && p[3] == "apps" && p[7] == "files" {
		app, err := getResource[recipeApp](a, "recipe_apps", p[4])
		if err != nil || app.WorkspaceID != p[2] {
			return t, p[8], true
		}
		switch p[5] {
		case "containers":
			for _, c := range app.Recipe.Containers {
				if c.ID == p[6] {
					return fileTarget{container: recipeContainer(app.ID, c.ID), workspace: app.WorkspaceID}, p[8], true
				}
			}
		case "volumes":
			if slices.Contains(app.Recipe.Volumes, p[6]) {
				return fileTarget{volume: recipeVolume(app.ID, p[6]), workspace: app.WorkspaceID}, p[8], true
			}
		}
		return t, p[8], true
	}
	return t, "", false
}

type fileCapabilities struct {
	Write  bool   `json:"write"`
	Move   bool   `json:"move"`
	Delete bool   `json:"delete"`
	Reason string `json:"reason,omitempty"`
}

type fileEntry struct {
	Name  string    `json:"name"`
	Type  string    `json:"type"`
	Size  int64     `json:"size"`
	Mode  int64     `json:"mode"`
	Mtime time.Time `json:"mtime"`
	Link  string    `json:"link,omitempty"`
}

type fileAction struct {
	To     string   `json:"to"`
	Format string   `json:"format"`
	Paths  []string `json:"paths"`
}

func (a *Server) filesAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	target, op, match := a.resolveFileTarget(p)
	if !match {
		return false
	}
	if target.workspace == "" || !x.memberOf(target.workspace) {
		fail(w, 404, "file target not found")
		return true
	}
	perms := x.masked(x.at(target.workspace, target.project), target.workspace)
	if !perms["files.read"] || r.Method != http.MethodGet && !perms["files.write"] {
		forbidden(w)
		return true
	}
	method := map[string]string{
		"list": "GET", "text": "GET", "download": "GET", "upload": "PUT", "save": "PUT",
		"mkdir": "POST", "create": "POST", "move": "POST", "delete": "DELETE",
		"compress": "POST", "extract": "POST",
	}[op]
	if method == "" || method != r.Method {
		fail(w, 405, "file operation not supported")
		return true
	}
	name, err := filePath(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, 400, err.Error())
		return true
	}
	if target.container == "" && target.volume == "" {
		fail(w, 409, "no runtime container is available")
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	fs, close, err := target.open(ctx, perms["files.write"] && (r.Method != "GET" || target.volume == ""))
	defer close()
	if err != nil {
		status := 502
		if errors.Is(err, errFileMissing) {
			status = 404
		}
		fail(w, status, err.Error())
		return true
	}
	if target.volume != "" && op == "list" {
		fs.write, fs.move = perms["files.write"], perms["files.write"]
		if !fs.write {
			fs.reason = "You have read-only file access."
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && (!fs.write || (op == "move" || op == "delete") && !fs.move) {
		fail(w, 409, fs.reason)
		return true
	}
	if err := fs.serve(ctx, w, r, op, name); err != nil {
		status := 400
		if errors.Is(err, errFileMissing) {
			status = 404
		}
		if errors.Is(err, errArchiveLimit) || errors.As(err, new(*http.MaxBytesError)) {
			status = 413
		}
		fail(w, status, err.Error())
	} else if r.Method != "GET" {
		scope, id := scopeWorkspace, target.workspace
		if target.project != "" {
			scope, id = scopeProject, target.project
		}
		a.audit(ctx, uid, "files."+op, name, scope, id, nil)
		reply(w, 200, map[string]bool{"ok": true})
	}
	return true
}

func (fs fileDocker) list(ctx context.Context, name string) ([]fileEntry, error) {
	entries := []fileEntry{}
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
				root = n + "/"
				if h.Typeflag != tar.TypeDir {
					return errors.New("not a directory")
				}
				continue
			}
			child, found := strings.CutPrefix(n, root)
			if !found || strings.Contains(child, "/") || child == "" {
				continue
			}
			kind := "special"
			switch {
			case regularFile(h):
				kind = "file"
			case h.Typeflag == tar.TypeDir:
				kind = "directory"
			case h.Typeflag == tar.TypeSymlink:
				kind = "symlink"
			case h.Typeflag == tar.TypeLink:
				kind = "hardlink"
			}
			entries = append(entries, fileEntry{child, kind, h.Size, h.Mode, h.ModTime, h.Linkname})
			if len(entries) > maxArchiveEntries {
				return errArchiveLimit
			}
		}
	})
	slices.SortFunc(entries, func(a, b fileEntry) int {
		if (a.Type == "directory") != (b.Type == "directory") {
			if a.Type == "directory" {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return entries, err
}

func textFile(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, c := range b {
		if (c < 32 && c != '\t' && c != '\n' && c != '\r') || c == 127 {
			return false
		}
	}
	return true
}

func (fs fileDocker) withFile(ctx context.Context, name string, f func(*tar.Header, io.Reader) error) error {
	return fs.read(ctx, name, func(t *tar.Reader) error {
		h, err := t.Next()
		if err != nil {
			return err
		}
		if !regularFile(h) {
			return errors.New("not a regular file")
		}
		return f(h, t)
	})
}

func fileAttachment(w http.ResponseWriter, name, mimeType string) {
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (fs fileDocker) serve(ctx context.Context, w http.ResponseWriter, r *http.Request, op, name string) error {
	switch op {
	case "list", "text", "download":
		h, err := fs.check(ctx, name)
		if err != nil {
			return err
		}
		if op == "list" {
			entries, err := fs.list(ctx, name)
			if err == nil {
				reply(w, 200, map[string]any{"entries": entries, "capabilities": fileCapabilities{fs.write, fs.move, fs.move, fs.reason}})
			}
			return err
		}
		if op == "download" && h.Typeflag == tar.TypeDir {
			format := r.URL.Query().Get("format")
			if format == "" {
				format = "zip"
			}
			return fs.downloadArchive(ctx, w, name, format)
		}
		if op == "download" {
			return fs.withFile(ctx, name, func(h *tar.Header, src io.Reader) error {
				fileAttachment(w, path.Base(name), "application/octet-stream")
				_, _ = io.Copy(w, src)
				return nil
			})
		}
		return fs.withFile(ctx, name, func(h *tar.Header, src io.Reader) error {
			if h.Size > maxFileText {
				return errors.New("file is too large for the editor; download it instead")
			}
			b, err := io.ReadAll(src)
			if err != nil {
				return err
			}
			if !textFile(b) {
				return errors.New("file is binary; download it instead")
			}
			reply(w, 200, map[string]string{"text": string(b)})
			return nil
		})
	}

	if name == "/" {
		return errFilePath
	}
	var action fileAction
	if (op == "move" || op == "compress" || op == "extract") && decode(w, r, &action) != nil {
		return errors.New("invalid file operation")
	}
	switch op {
	case "mkdir", "create", "upload", "save":
		return fs.upload(ctx, w, r, op, name)
	case "move", "delete":
		if _, err := fs.check(ctx, name); err != nil {
			return err
		}
		if op == "move" {
			dst, err := filePath(action.To)
			if err != nil || dst == "/" || strings.HasPrefix(dst, name+"/") {
				return errFilePath
			}
			if err := fs.destination(ctx, dst, false); err != nil {
				return err
			}
			return fs.mutate(ctx, op, name, dst)
		}
		return fs.mutate(ctx, op, name, "")
	case "compress":
		return fs.compress(ctx, name, action)
	case "extract":
		return fs.extract(ctx, name, action.To)
	}
	return errors.New("unsupported file operation")
}

func (fs fileDocker) destination(ctx context.Context, name string, overwrite bool) error {
	h, err := fs.check(ctx, path.Dir(name))
	if err != nil {
		return err
	}
	if h.Typeflag != tar.TypeDir {
		return errFilePath
	}
	h, err = fs.check(ctx, name)
	if errors.Is(err, errFileMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	if overwrite && regularFile(h) {
		return nil
	}
	return errors.New("destination already exists")
}

func stageFile() (*os.File, func(), error) {
	f, err := os.CreateTemp("", "startship-files-*")
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { f.Close(); os.Remove(f.Name()) }, nil
}

func (fs fileDocker) put(ctx context.Context, name string, h *tar.Header, src io.Reader) error {
	h.Name = path.Base(name)
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() {
		t := tar.NewWriter(w)
		err := t.WriteHeader(h)
		if err == nil && src != nil {
			_, err = io.Copy(t, src)
		}
		err = errors.Join(err, t.Close())
		w.CloseWithError(err)
		done <- err
	}()
	err := fs.writeTar(ctx, path.Dir(name), r)
	r.Close()
	return errors.Join(err, <-done)
}

func (fs fileDocker) upload(ctx context.Context, w http.ResponseWriter, r *http.Request, op, name string) error {
	if err := fs.destination(ctx, name, op == "upload" || op == "save"); err != nil {
		return err
	}
	h := &tar.Header{Typeflag: tar.TypeReg, Mode: 0644, ModTime: time.Now()}
	switch op {
	case "mkdir":
		h.Typeflag, h.Mode = tar.TypeDir, 0755
		return fs.put(ctx, name, h, nil)
	case "create":
		return fs.put(ctx, name, h, nil)
	}
	if existing, err := fs.stat(ctx, name); err == nil {
		h.Mode, h.Uid, h.Gid = existing.Mode&0777, existing.Uid, existing.Gid
	} else if op == "save" || !errors.Is(err, errFileMissing) {
		return err
	}
	f, cleanup, err := stageFile()
	defer cleanup()
	if err != nil {
		return err
	}
	limit := int64(maxFileTransfer)
	if op == "save" {
		limit = maxFileText
		if err := fs.withFile(ctx, name, func(h *tar.Header, src io.Reader) error {
			b, err := io.ReadAll(io.LimitReader(src, maxFileText+1))
			if err == nil && (len(b) > maxFileText || !textFile(b)) {
				return errors.New("existing file is not editable text")
			}
			return err
		}); err != nil {
			return err
		}
	}
	h.Size, err = io.Copy(f, http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if op == "save" {
		b, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		if !textFile(b) {
			return errors.New("only UTF-8 text can be saved")
		}
		return fs.put(ctx, name, h, strings.NewReader(string(b)))
	}
	return fs.put(ctx, name, h, f)
}
