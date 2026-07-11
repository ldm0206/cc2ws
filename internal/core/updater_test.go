package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"strings"
	"testing"
)

type fakeGitHub struct {
	release  releaseManifest
	assetBuf []byte
	checksum string
	err      error
}

func (f *fakeGitHub) LatestRelease(ctx context.Context, repo string) (releaseManifest, error) {
	if f.err != nil {
		return releaseManifest{}, f.err
	}
	return f.release, nil
}
func (f *fakeGitHub) Download(ctx context.Context, url string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.assetBuf, nil
}
func (f *fakeGitHub) Checksums(ctx context.Context, repo, tag string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.checksum, nil
}

func TestAssetNameForPlatform(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"windows", "amd64", "cc2ws-windows-amd64.exe"},
		{"darwin", "amd64", "cc2ws-darwin-amd64"},
		{"darwin", "arm64", "cc2ws-darwin-arm64"},
		{"linux", "amd64", "cc2ws-linux-amd64"},
		{"linux", "arm64", "cc2ws-linux-arm64"},
	}
	for _, c := range cases {
		if got := assetName(c.goos, c.goarch); got != c.want {
			t.Errorf("assetName(%s,%s)=%q want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		newer           bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.0", false},
		{"dev", "v0.1.0", true},
	}
	for _, c := range cases {
		if got := versionNewer(c.current, c.latest); got != c.newer {
			t.Errorf("versionNewer(%s,%s)=%v want %v", c.current, c.latest, got, c.newer)
		}
	}
}

func TestCheckFindsUpdate(t *testing.T) {
	name := assetName(runtime.GOOS, runtime.GOARCH)
	fg := &fakeGitHub{
		release:  releaseManifest{TagName: "v0.2.0", Assets: []asset{{Name: name, URL: "https://x/" + name}}},
		checksum: "deadbeef  " + name,
	}
	u := &Updater{repo: "o/cc2ws", currentVersion: "v0.1.0", gh: fg}
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if info.Version != "v0.2.0" {
		t.Errorf("Version=%q", info.Version)
	}
	if !strings.Contains(info.AssetURL, name) {
		t.Errorf("AssetURL=%q", info.AssetURL)
	}
	if info.ChecksumLine == "" {
		t.Error("ChecksumLine empty")
	}
}

func TestCheckNoUpdate(t *testing.T) {
	fg := &fakeGitHub{release: releaseManifest{TagName: "v0.1.0"}}
	u := &Updater{repo: "o/cc2ws", currentVersion: "v0.1.0", gh: fg}
	_, err := u.Check(context.Background())
	if !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("want ErrNoUpdate, got %v", err)
	}
}

func TestVerifyChecksum(t *testing.T) {
	body := []byte("hello world")
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	if err := verifyChecksum(body, hexSum); err != nil {
		t.Fatalf("match should pass: %v", err)
	}
	if err := verifyChecksum(body, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("mismatch should fail")
	}
}

func TestParseChecksumLine(t *testing.T) {
	name := assetName(runtime.GOOS, runtime.GOARCH)
	line := "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234  " + name
	sum, ok := parseChecksumLine(line, name)
	if !ok {
		t.Fatal("should match")
	}
	if sum != "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234" {
		t.Errorf("sum=%q", sum)
	}
	if _, ok := parseChecksumLine(line, "cc2ws-other-amd64"); ok {
		t.Fatal("should not match a different asset name")
	}
}
