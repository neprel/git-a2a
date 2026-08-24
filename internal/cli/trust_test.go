package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/cache"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

type trustUpdateRunner struct{ commit string }

func (r trustUpdateRunner) Run(_ context.Context, _ string, _ []byte, _ ...string) ([]byte, error) {
	return []byte(r.commit + "\trefs/heads/main\n"), nil
}

func TestUpdateWarnsForUnsignedRequiredCardWhenCurrent(t *testing.T) {
	root := t.TempDir()
	commit := strings.Repeat("a", 40)
	dependency := manifest.Dependency{ID: "acme-lib", Git: "https://example.test/acme/lib.git", Ref: "main", Path: ".", Track: "locked"}
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer"}, Dependencies: []manifest.Dependency{dependency}}
	depManifest := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "acme-lib"}, Agents: []manifest.Agent{{
		Name: "acme-lib-utils", Role: "owner", Card: "cards/agent.json", Trust: &manifest.Trust{Signatures: true},
	}}}
	ownRaw, _ := manifest.Marshal(own)
	depRaw, _ := manifest.Marshal(depManifest)
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), ownRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cache.Save(root, dependency.ID, depRaw, commit, "test"); err != nil {
		t.Fatal(err)
	}
	cardRaw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	cardsDir := filepath.Join(cache.Dir(root, dependency.ID), "cards")
	if err = os.MkdirAll(cardsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(cardsDir, a2a.FileName("acme-lib-utils")), cardRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(depRaw)
	cardHash := sha256.Sum256(cardRaw)
	if err = lockfile.Write(root, &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{
		dependency.ID: {
			Git: dependency.Git, Ref: dependency.Ref, Path: dependency.Path, Commit: commit,
			Manifest: "sha256:" + hex.EncodeToString(manifestHash[:]),
			Cards:    map[string]string{"acme-lib-utils": "sha256:" + hex.EncodeToString(cardHash[:])},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	app.Runner = trustUpdateRunner{commit: commit}
	if code := app.Run([]string{"update"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "dependencies are up to date") || !strings.Contains(errOut.String(), "warning: acme-lib card trust: acme-lib-utils: card is unsigned") {
		t.Fatalf("stderr:\n%s", errOut.String())
	}
}

func TestTrustShowIsDeterministicAndReportsPinnedPolicy(t *testing.T) {
	root := t.TempDir()
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer"}, Dependencies: []manifest.Dependency{{
		ID: "acme-lib", Git: "https://example.test/acme/lib.git", Ref: "main",
		Require: &manifest.Require{Commits: "signed", Signers: "trust/allowed_signers", Cards: "signed", CardOrigin: true},
	}}}
	raw, _ := manifest.Marshal(own)
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	locked := &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{"acme-lib": {
		Git: "https://example.test/acme/lib.git", Ref: "main", Path: ".",
		Commit: strings.Repeat("a", 40), Manifest: "sha256:" + strings.Repeat("b", 64), Verified: "signed",
		CardsKeys: map[string]manifest.LockedCardKey{
			"z-agent": {KeyID: "z", Thumbprint: "z-thumb", FirstSeen: strings.Repeat("a", 40)},
			"a-agent": {KeyID: "a", Thumbprint: "a-thumb", FirstSeen: strings.Repeat("a", 40)},
		},
	}}}
	if err := lockfile.Write(root, locked); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"trust", "show", "acme-lib"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "commits signed (signed), cards signed, origin-required true") {
		t.Fatalf("output = %q", out.String())
	}
	if strings.Index(out.String(), "a-agent") > strings.Index(out.String(), "z-agent") {
		t.Fatalf("card keys are not sorted:\n%s", out.String())
	}
}

func TestChangedJSONFieldsReportsCardInterfaceDrift(t *testing.T) {
	oldCard := []byte(`{"name":"acme-lib","supportedInterfaces":[{"url":"https://old.example/a2a","protocolVersion":"1.0"}]}`)
	newCard := []byte(`{"name":"acme-lib","supportedInterfaces":[{"url":"https://new.example/a2a","protocolVersion":"1.0"}]}`)
	fields := changedJSONFields(oldCard, newCard)
	if len(fields) != 1 || fields[0] != "/supportedInterfaces" {
		t.Fatalf("changed fields = %v", fields)
	}
}
