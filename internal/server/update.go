package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

var githubSlug = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

type Updates struct {
	updater    *selfupdate.Updater
	repository selfupdate.RepositorySlug
	version    string
	executable string
	mu         sync.Mutex
	installed  string
}

type updateStatus struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	Available       bool   `json:"available"`
	RestartRequired bool   `json:"restartRequired"`
}

func NewUpdates(repository, version, executable string) (*Updates, error) {
	if repository == "" {
		return nil, nil
	}
	if !githubSlug.MatchString(repository) {
		return nil, errors.New("GitHub 更新仓库应使用 owner/repo 格式")
	}
	updater, err := newUpdater(nil)
	if err != nil {
		return nil, err
	}
	return &Updates{updater: updater, repository: selfupdate.ParseSlug(repository), version: version, executable: executable}, nil
}

func newUpdater(source selfupdate.Source) (*selfupdate.Updater, error) {
	archive := "deadlock-translator-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	return selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Filters:   []string{"^" + regexp.QuoteMeta(archive) + "$"},
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "SHA256SUMS.txt"},
	})
}

var errUpdateChanged = errors.New("GitHub 上的更新版本已变化，请重新检查")

// run is called with the update lock held. A requested version pins installation
// to the release that the user saw during the check.
func (u *Updates) run(ctx context.Context, requested string) (updateStatus, error) {
	state := updateStatus{Current: u.version}
	if u.installed != "" {
		state.Latest, state.RestartRequired = u.installed, true
		return state, nil
	}
	latest, found, err := u.updater.DetectLatest(ctx, u.repository)
	if err != nil {
		return state, err
	}
	if found {
		state.Latest = latest.Version()
		state.Available = latest.GreaterThan(u.version)
	}
	if requested == "" {
		return state, nil
	}
	if !state.Available || requested != state.Latest {
		return state, errUpdateChanged
	}
	executable, err := filepath.EvalSymlinks(u.executable)
	if err != nil {
		return state, err
	}
	if err = u.updater.UpdateTo(ctx, latest, executable); err != nil {
		return state, err
	}
	u.installed = state.Latest
	state.Available, state.RestartRequired = false, true
	return state, nil
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	u := s.updates
	if u == nil {
		fail(w, http.StatusNotFound, "当前构建未指定 GitHub 更新仓库")
		return
	}
	var requested string
	timeout := 12 * time.Second
	if r.URL.Path == "/api/v1/update/install" {
		var req struct {
			Version string `json:"version"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.Version == "" || len(req.Version) > 64 {
			fail(w, http.StatusBadRequest, "请提供已检查的更新版本")
			return
		}
		requested = req.Version
		timeout = 2 * time.Minute
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout + 5*time.Second))
	}
	if !u.mu.TryLock() {
		fail(w, http.StatusConflict, "另一项更新操作正在进行")
		return
	}
	defer u.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	state, err := u.run(ctx, requested)
	if errors.Is(err, errUpdateChanged) {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		slog.Warn("GitHub 更新失败", "error", err)
		fail(w, http.StatusBadGateway, "GitHub 更新失败，详情见 Go 桥终端")
		return
	}
	send(w, http.StatusOK, state)
}
