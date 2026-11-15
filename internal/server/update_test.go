package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

type updateFixture struct {
	releases []selfupdate.SourceRelease
	files    map[int64][]byte
}

func (f *updateFixture) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return f.releases, nil
}

func (f *updateFixture) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, id int64) (io.ReadCloser, error) {
	if contents, ok := f.files[id]; ok {
		return io.NopCloser(bytes.NewReader(contents)), nil
	}
	return nil, fmt.Errorf("unexpected download: %d", id)
}

type releaseFixture struct {
	tag        string
	prerelease bool
	assets     []selfupdate.SourceAsset
}

func (r releaseFixture) GetID() int64                        { return 1 }
func (r releaseFixture) GetTagName() string                  { return r.tag }
func (r releaseFixture) GetDraft() bool                      { return false }
func (r releaseFixture) GetPrerelease() bool                 { return r.prerelease }
func (r releaseFixture) GetPublishedAt() time.Time           { return time.Time{} }
func (r releaseFixture) GetReleaseNotes() string             { return "" }
func (r releaseFixture) GetName() string                     { return r.tag }
func (r releaseFixture) GetURL() string                      { return "" }
func (r releaseFixture) GetAssets() []selfupdate.SourceAsset { return r.assets }

type assetFixture struct {
	id   int64
	name string
}

func (a assetFixture) GetID() int64                  { return a.id }
func (a assetFixture) GetName() string               { return a.name }
func (a assetFixture) GetSize() int                  { return 0 }
func (a assetFixture) GetBrowserDownloadURL() string { return "https://example.invalid/" + a.name }

func TestBridgeInstallUpdate(t *testing.T) {
	name := "deadlock-translator"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(executable, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	member, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(member, "new"); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	packageName := "deadlock-translator-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	assets := []selfupdate.SourceAsset{
		assetFixture{1, "Deadlock-AI-Translator-Go.zip"},
		assetFixture{2, packageName},
		assetFixture{3, "SHA256SUMS.txt"},
	}
	fixture := &updateFixture{
		releases: []selfupdate.SourceRelease{releaseFixture{"v2.0.0-beta.1", true, assets}, releaseFixture{"v1.2.10", false, assets}},
		files:    map[int64][]byte{2: data.Bytes(), 3: []byte(fmt.Sprintf("%064d  %s\n", 0, packageName))},
	}
	updater, err := newUpdater(fixture)
	if err != nil {
		t.Fatal(err)
	}
	u := &Updates{updater: updater, repository: selfupdate.ParseSlug("example/repo"), version: "1.2.9", executable: executable}
	ctx := context.Background()
	state, err := u.run(ctx, "")
	if err != nil || !state.Available || state.Latest != "1.2.10" {
		t.Fatalf("check = %+v, %v", state, err)
	}
	if _, err = u.run(ctx, "1.2.8"); !errors.Is(err, errUpdateChanged) {
		t.Fatalf("stale version installation error = %v", err)
	}
	if _, err = u.run(ctx, state.Latest); err == nil {
		t.Fatal("package with a mismatched checksum was installed")
	}
	assertExecutable := func(want string) {
		t.Helper()
		contents, err := os.ReadFile(executable)
		if err != nil || string(contents) != want {
			t.Fatalf("executable = %q, %v, want %q", contents, err, want)
		}
	}
	assertExecutable("old")
	fixture.files[3] = []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(data.Bytes()), packageName))
	state, err = u.run(ctx, state.Latest)
	if err != nil || !state.RestartRequired || state.Available {
		t.Fatalf("install = %+v, %v", state, err)
	}
	assertExecutable("new")
	if state, err = u.run(ctx, ""); err != nil || !state.RestartRequired || state.Available {
		t.Fatalf("check before restart = %+v, %v", state, err)
	}
}

func TestUpdateInstallRequiresLocalAdminRequest(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8791/api/v1/update/install", strings.NewReader(`{"version":"1.2.10"}`))
	result := httptest.NewRecorder()
	s.Handler(false).ServeHTTP(result, req)
	if result.Code != http.StatusForbidden {
		t.Fatalf("install without bridge request header = %d", result.Code)
	}
	req.Header.Set("X-Deadlock-Bridge", "1")
	result = httptest.NewRecorder()
	s.Handler(true).ServeHTTP(result, req)
	if result.Code != http.StatusNotFound {
		t.Fatalf("install via game listener = %d", result.Code)
	}
}
