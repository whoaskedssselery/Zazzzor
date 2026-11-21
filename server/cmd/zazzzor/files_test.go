package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// filesEnv is the environment of `zazzzor files` for cfg.
func filesEnv(cfg config.Config) func(string) string {
	return func(k string) string {
		return map[string]string{
			"ZAZZZOR_DATABASE_URL":    cfg.DatabaseURL,
			"ZAZZZOR_ALLOWED_ORIGINS": cfg.AllowedOrigins[0],
			"ZAZZZOR_DATA_DIR":        cfg.DataDir,
		}[k]
	}
}

// newFilesModules wires the modules on a fresh database and data directory,
// and returns the configuration that reaches both.
func newFilesModules(t *testing.T) (*app.Modules, config.Config) {
	t.Helper()
	pool := testkit.Postgres(t)
	testkit.OpenRegistration(t, pool)
	cfg := testConfig()
	cfg.DatabaseURL = pool.Config().ConnString()
	cfg.DataDir = t.TempDir()
	m, err := app.NewModules(cfg, pool, snowflake.NewGenerator(0, time.Now), nil)
	if err != nil {
		t.Fatalf("NewModules: %v", err)
	}
	return m, cfg
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return b.Bytes()
}

// oldOrphan writes an unreferenced icon older than files.ReconcileGrace
// and returns its path.
func oldOrphan(t *testing.T, dataDir string) string {
	t.Helper()
	path := filepath.Join(dataDir, "icons", "42", "0123456789abcdef.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, testPNG(t), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	old := time.Now().Add(-2 * files.ReconcileGrace)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return path
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat: %v", err)
	}
	return err == nil
}

func TestFilesReconcileRestoresReferencedImages(t *testing.T) {
	// The database of a restore still refers to images that were deleted
	// after its dump was taken.
	m, cfg := newFilesModules(t)
	u, _, err := m.Auth.Register(t.Context(), auth.RegisterInput{Username: "restore", Password: "password123"}, auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u, err = m.Users.SetAvatar(t.Context(), u.ID, testPNG(t)); err != nil {
		t.Fatalf("SetAvatar: %v", err)
	}
	g, err := m.Guilds.CreateGuild(t.Context(), u, "restore", "")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	icon, err := m.Guilds.SetIcon(t.Context(), g.Guild.ID, u.ID, testPNG(t))
	if err != nil {
		t.Fatalf("SetIcon: %v", err)
	}
	if err := m.Files.Delete(files.KindAvatar, u.ID, *u.AvatarHash); err != nil {
		t.Fatalf("Delete avatar: %v", err)
	}
	if err := m.Files.Delete(files.KindIcon, icon.ID, *icon.IconHash); err != nil {
		t.Fatalf("Delete icon: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"files", "reconcile"}, filesEnv(cfg), &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, &stderr)
	}
	for _, p := range []string{
		filepath.Join(cfg.DataDir, "avatars", u.ID.String(), *u.AvatarHash+".png"),
		filepath.Join(cfg.DataDir, "icons", icon.ID.String(), *icon.IconHash+".png"),
	} {
		if !exists(t, p) {
			t.Errorf("%s was not moved back from trash", p)
		}
	}
	if !strings.Contains(stdout.String(), "restored 2") {
		t.Errorf("stdout %q does not report 2 restored images", stdout.String())
	}
}

func TestFilesReconcileTrashesOldOrphan(t *testing.T) {
	_, cfg := newFilesModules(t)
	orphan := oldOrphan(t, cfg.DataDir)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"files", "reconcile"}, filesEnv(cfg), &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, &stderr)
	}
	if exists(t, orphan) {
		t.Error("the old orphan is still live")
	}
	if !strings.Contains(stdout.String(), "trashed 1") {
		t.Errorf("stdout %q does not report 1 trashed image", stdout.String())
	}
}

func TestStrictReconcileRejectsMissingReferencedFiles(t *testing.T) {
	m, cfg := newFilesModules(t)
	u, _, err := m.Auth.Register(t.Context(), auth.RegisterInput{Username: "restore", Password: "password123"}, auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	u, err = m.Users.SetAvatar(t.Context(), u.ID, testPNG(t))
	if err != nil {
		t.Fatalf("SetAvatar: %v", err)
	}
	path := filepath.Join(cfg.DataDir, "avatars", u.ID.String(), *u.AvatarHash+".png")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove live avatar: %v", err)
	}
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"files", "reconcile"}, 0},
		{[]string{"files", "reconcile", "--strict"}, 1},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, filesEnv(cfg), &stdout, &stderr); code != tc.want ||
			!strings.Contains(stderr.String(), files.ErrMissingImage.Error()) {
			t.Errorf("%v exit = %d, want %d with missing-file diagnostic: %s", tc.args, code, tc.want, &stderr)
		}
	}
}

func TestFilesReconcileReportsFailedItems(t *testing.T) {
	_, cfg := newFilesModules(t)
	orphan := oldOrphan(t, cfg.DataDir)
	// A file where the orphan's trash directory must go.
	blocker := filepath.Join(cfg.DataDir, "trash", "icons", "42")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"files", "reconcile"}, filesEnv(cfg), &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1; stdout: %s", code, &stdout)
	}
	if !strings.Contains(stderr.String(), "icons/42/0123456789abcdef.png") {
		t.Errorf("stderr %q does not name the failed file", stderr.String())
	}
	if !strings.Contains(stdout.String(), "failed 1") {
		t.Errorf("stdout %q has no summary with 1 failure", stdout.String())
	}
	if !exists(t, orphan) {
		t.Error("the file that could not be moved is gone")
	}
}

func TestFilesReconcileTouchesNothingWithoutDatabase(t *testing.T) {
	defer swapRetryDelay(10 * time.Millisecond)()
	defer swapStartupTimeout(time.Second)()
	cfg := testConfig()
	cfg.DatabaseURL = "postgres://zz:pw@127.0.0.1:1/zz?connect_timeout=2" //nolint:gosec // fake credentials for a closed port
	cfg.DataDir = t.TempDir()
	orphan := oldOrphan(t, cfg.DataDir)
	var stderr bytes.Buffer
	if code := run([]string{"files", "reconcile"}, filesEnv(cfg), &bytes.Buffer{}, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !exists(t, orphan) {
		t.Error("a file was moved although the database was unreachable")
	}
	if !strings.Contains(stderr.String(), "open database") {
		t.Errorf("stderr %q does not name the cause", stderr.String())
	}
	if strings.Contains(stderr.String(), ":pw@") {
		t.Errorf("stderr leaks the database password: %s", stderr.String())
	}
}

func TestFilesCommandUsage(t *testing.T) {
	for _, args := range [][]string{{"files"}, {"files", "purge"}, {"files", "reconcile", "now"}} {
		var stderr bytes.Buffer
		if code := run(args, filesEnv(testConfig()), &bytes.Buffer{}, &stderr); code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "zazzzor files reconcile") {
			t.Errorf("%v: stderr %q has no usage", args, stderr.String())
		}
	}
}

func TestStartFilePurgeStopWaitsForTheJob(t *testing.T) {
	disk, err := files.NewDisk(t.TempDir(), files.Options{Retention: time.Hour})
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	stop := startFilePurge(t.Context(), disk, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
}
