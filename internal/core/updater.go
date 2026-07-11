package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"github.com/minio/selfupdate"
)

var ErrNoUpdate = errors.New("cc2ws is up to date")

// Repo is the GitHub owner/repo to check for releases.
var Repo = "ldm0206/cc2ws"

// UpdateInfo describes a downloadable newer release.
type UpdateInfo struct {
	Version      string
	AssetURL     string
	ChecksumLine string // the full "<hex>  <filename>" line
	ReleaseURL   string
}

// GitHubAPI is the subset of GitHub the updater needs, as an interface so
// tests inject a fake (no network).
type GitHubAPI interface {
	LatestRelease(ctx context.Context, repo string) (releaseManifest, error)
	Download(ctx context.Context, url string) ([]byte, error)
	Checksums(ctx context.Context, repo, tag string) (string, error)
}

type releaseManifest struct {
	TagName    string
	Assets     []asset
	Body       string
	ReleaseURL string
}

type asset struct {
	Name string
	URL  string
}

// Updater checks GitHub Releases and self-applies verified binaries.
type Updater struct {
	repo           string
	currentVersion string
	gh             GitHubAPI
}

// NewUpdater constructs an Updater for the repo using the default Version and
// an HTTP-backed GitHub client.
func NewUpdater() *Updater {
	return &Updater{repo: Repo, currentVersion: Version, gh: newHTTPGitHub()}
}

type httpGitHub struct{ apiBase string }

func newHTTPGitHub() *httpGitHub { return &httpGitHub{apiBase: "https://api.github.com"} }

func (h *httpGitHub) LatestRelease(ctx context.Context, repo string) (releaseManifest, error) {
	u := fmt.Sprintf("%s/repos/%s/releases/latest", h.apiBase, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return releaseManifest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseManifest{}, err
	}
	defer resp.Body.Close()
	var api struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&api); err != nil {
		return releaseManifest{}, err
	}
	m := releaseManifest{TagName: api.TagName, Body: api.Body, ReleaseURL: api.HTMLURL}
	for _, a := range api.Assets {
		m.Assets = append(m.Assets, asset{Name: a.Name, URL: a.BrowserDownloadURL})
	}
	return m, nil
}

func (h *httpGitHub) Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (h *httpGitHub) Checksums(ctx context.Context, repo, tag string) (string, error) {
	u := fmt.Sprintf("https://github.com/%s/releases/download/%s/checksums.txt", repo, tag)
	b, err := h.Download(ctx, u)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// assetName returns the release asset filename for a GOOS/GOARCH.
func assetName(goos, goarch string) string {
	name := fmt.Sprintf("cc2ws-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// versionNewer reports whether latest is strictly newer than current. A
// "dev" current version is always considered older (a dev build always
// sees an update).
func versionNewer(current, latest string) bool {
	if current == "dev" {
		return true
	}
	ct := strings.TrimPrefix(current, "v")
	lt := strings.TrimPrefix(latest, "v")
	var c1, c2, c3, l1, l2, l3 int
	fmt.Sscanf(ct, "%d.%d.%d", &c1, &c2, &c3)
	fmt.Sscanf(lt, "%d.%d.%d", &l1, &l2, &l3)
	if l1 != c1 {
		return l1 > c1
	}
	if l2 != c2 {
		return l2 > c2
	}
	return l3 > c3
}

// Check queries the latest release and returns its info if newer than current.
// Returns ErrNoUpdate if current is already up to date.
func (u *Updater) Check(ctx context.Context) (UpdateInfo, error) {
	m, err := u.gh.LatestRelease(ctx, u.repo)
	if err != nil {
		return UpdateInfo{}, err
	}
	if !versionNewer(u.currentVersion, m.TagName) {
		return UpdateInfo{}, ErrNoUpdate
	}
	want := assetName(runtime.GOOS, runtime.GOARCH)
	info := UpdateInfo{Version: m.TagName, ReleaseURL: m.ReleaseURL}
	for _, a := range m.Assets {
		if a.Name == want {
			info.AssetURL = a.URL
		}
	}
	if info.AssetURL == "" {
		return UpdateInfo{}, fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	checksums, err := u.gh.Checksums(ctx, u.repo, m.TagName)
	if err != nil {
		return UpdateInfo{}, err
	}
	if line, ok := findChecksumLine(checksums, want); ok {
		info.ChecksumLine = line
	}
	if info.ChecksumLine == "" {
		return UpdateInfo{}, fmt.Errorf("no checksum for %s in checksums.txt", want)
	}
	return info, nil
}

// Apply downloads the asset, verifies its SHA256 against the checksum line,
// then self-applies the verified binary. Checksum verification is ALWAYS
// enforced before selfupdate.Apply — there is no path that applies an
// unverified binary.
func (u *Updater) Apply(ctx context.Context, info UpdateInfo) error {
	body, err := u.gh.Download(ctx, info.AssetURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	sum, ok := parseChecksumLine(info.ChecksumLine, assetName(runtime.GOOS, runtime.GOARCH))
	if !ok {
		return fmt.Errorf("checksum line for %s malformed", assetName(runtime.GOOS, runtime.GOARCH))
	}
	if err := verifyChecksum(body, sum); err != nil {
		return fmt.Errorf("checksum verify: %w", err)
	}
	if err := selfupdate.Apply(bytes.NewReader(body), selfupdate.Options{}); err != nil {
		return fmt.Errorf("selfupdate: %w", err)
	}
	return nil
}

// verifyChecksum returns nil if sha256(body) == sum, else an error.
func verifyChecksum(body []byte, sum string) error {
	h := sha256.Sum256(body)
	got := hex.EncodeToString(h[:])
	if got != sum {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, sum)
	}
	return nil
}

// parseChecksumLine parses "<hex>  <filename>" and returns the hex if the
// filename matches name, else ok=false.
func parseChecksumLine(line, name string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) != 2 || parts[1] != name {
		return "", false
	}
	return parts[0], true
}

// findChecksumLine scans a checksums.txt blob for the line matching name.
func findChecksumLine(checksums, name string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		if _, ok := parseChecksumLine(line, name); ok {
			return line, true
		}
	}
	return "", false
}
