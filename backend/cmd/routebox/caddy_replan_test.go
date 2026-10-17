package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routebox/backend/internal/bootstrap"
	"routebox/backend/internal/config"
)

// staleInstall is a fresh all-in-one install whose Caddyfile has since been
// replaced by something the planner would never write, and whose dest is
// unreachable — so SyncCaddyfile writes without asking Caddy.
func staleInstall(t *testing.T) (func() error, allInOne) {
	t.Helper()
	sm, a, _ := installed(t)
	if err := os.WriteFile(a.CaddyPath, []byte("# stale\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(bootstrap.CaddyAdminEnv, "127.0.0.1:1")
	cfg, err := config.NewManager(a.ConfigPath)
	if err != nil {
		t.Fatalf("load the planned config: %v", err)
	}
	return caddyReplanner(sm, cfg, 8443, a.StubRoot), a
}

func TestCaddyReplannerRewritesAStaleCaddyfile(t *testing.T) {
	replan, a := staleInstall(t)
	if err := replan(); err != nil {
		t.Fatalf("replan: %v", err)
	}
	got, err := os.ReadFile(a.CaddyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), bootstrap.CaddyfileHeader) {
		t.Errorf("the re-planned Caddyfile has no header:\n%s", got)
	}
	if !strings.Contains(string(got), "reverse_proxy 127.0.0.1:9446 {") {
		t.Errorf("the re-planned Caddyfile does not reach the web front:\n%s", got)
	}
}

// BOOTSTRAP_CADDYFILE_REPLAN=0 is the operator's promise that the file is
// theirs now.
func TestCaddyReplannerIsANoOpWhenDisabled(t *testing.T) {
	t.Setenv("BOOTSTRAP_CADDYFILE_REPLAN", "0")
	replan, a := staleInstall(t)
	if err := replan(); err != nil {
		t.Fatalf("replan: %v", err)
	}
	got, err := os.ReadFile(a.CaddyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# stale\n" {
		t.Errorf("a disabled re-plan rewrote the Caddyfile:\n%s", got)
	}
}

// A plain VPS has no dest of ours to plan for.
func TestCaddyReplannerIsANoOpOffBootstrap(t *testing.T) {
	dir := t.TempDir()
	sm := newVPSSettings(t, dir)
	replan := caddyReplanner(sm, config.NewEmptyManager(filepath.Join(dir, "config.json")), 8443, filepath.Join(dir, "stub"))
	if err := replan(); err != nil {
		t.Fatalf("replan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Caddyfile")); err == nil {
		t.Error("a re-plan off a bootstrapped install wrote a Caddyfile")
	}
}
