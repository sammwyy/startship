package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestFilePath(t *testing.T) {
	for _, p := range []string{"", "/", "/etc/app config.json", "/é/文"} {
		if _, err := filePath(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"relative", "/../file", "/x/./y", "/x//y", "/x/", "/x\\y", "/x\x00y"} {
		if _, err := filePath(p); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	if !textFile([]byte("á\n\t{}")) || textFile([]byte{0, 1, 2}) || textFile([]byte{0xff}) {
		t.Fatal("incorrect text detection")
	}
}

func TestFileArchiveRoundTrip(t *testing.T) {
	for _, format := range []string{"zip", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			in, cleanup, err := stageFile()
			defer cleanup()
			if err != nil {
				t.Fatal(err)
			}
			if err := writeFileArchive(in, format, func(sink fileArchiveSink) error {
				if err := sink(&tar.Header{Name: "config", Mode: 0755, Typeflag: tar.TypeDir}, strings.NewReader("")); err != nil {
					return err
				}
				return sink(&tar.Header{Name: "config/test.json", Mode: 0640, Size: 2, Typeflag: tar.TypeReg}, strings.NewReader("{}"))
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := in.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			if err := normalizeFileArchive(in, "test."+format, &b, func(string) error { return nil }); err != nil {
				t.Fatal(err)
			}
			r := tar.NewReader(&b)
			dir, err := r.Next()
			if err != nil || dir.Name != "config" || dir.Typeflag != tar.TypeDir {
				t.Fatal(dir, err)
			}
			file, err := r.Next()
			if err != nil || file.Name != "config/test.json" || file.Mode != 0640 {
				t.Fatal(file, err)
			}
			content, err := io.ReadAll(r)
			if err != nil || string(content) != "{}" {
				t.Fatal(string(content), err)
			}
		})
	}
}

func TestFileArchiveRejectsUnsafePaths(t *testing.T) {
	for _, bad := range []string{"../escape", "/absolute", "config/../../escape", "config\\..\\escape", "C:/drive"} {
		t.Run(bad, func(t *testing.T) {
			in, cleanup, err := stageFile()
			defer cleanup()
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(in)
			if _, err := z.Create(bad); err != nil {
				t.Fatal(err)
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			if err := normalizeFileArchive(in, "unsafe.zip", io.Discard, func(string) error { return nil }); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestFilePermissions(t *testing.T) {
	f := newRBACFixture(t)
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	for _, v := range []Volume{{ID: "workspace-vol", WorkspaceID: f.workspace.ID}, {ID: "project-vol", WorkspaceID: f.workspace.ID, ProjectID: f.project.ID}} {
		if err := f.a.saveVolume(v); err != nil {
			t.Fatal(err)
		}
	}
	accountRequest(t, f.h, "GET", "volumes/project-vol/files/list", "", f.bob, 403)
	body, _ := json.Marshal(override{Scope: scopeProject, ScopeID: f.project.ID, SubjectType: "user", SubjectID: f.bobID, Allow: []string{"files.read"}})
	accountRequest(t, f.h, "PUT", "overrides", string(body), f.alice, 200)
	accountRequest(t, f.h, "GET", "volumes/workspace-vol/files/list", "", f.bob, 403)
	accountRequest(t, f.h, "GET", "volumes/project-vol/files/list?path=relative", "", f.bob, 400)
	accountRequest(t, f.h, "PUT", "volumes/project-vol/files/upload?path=relative", "", f.bob, 403)
	accountRequest(t, f.h, "PUT", "volumes/project-vol/files/upload?path=relative", "", f.alice, 400)
	body, _ = json.Marshal(override{Scope: scopeProject, ScopeID: f.project.ID, SubjectType: "user", SubjectID: f.bobID, Deny: []string{"files.read"}})
	accountRequest(t, f.h, "PUT", "overrides", string(body), f.alice, 200)
	accountRequest(t, f.h, "GET", "volumes/project-vol/files/list", "", f.bob, 403)
}
