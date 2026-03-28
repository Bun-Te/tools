package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const githubLatestReleaseAPI = "https://api.github.com/repos/mnemoo/tools/releases/latest"

// UpdateCheckResult is returned to the frontend after comparing local and remote versions.
type UpdateCheckResult struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	ReleaseURL      string `json:"releaseUrl"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Skipped         bool   `json:"skipped"`
	UpToDate        bool   `json:"upToDate"`
	CheckFailed     bool   `json:"checkFailed"`
}

type githubReleasePayload struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// GetAppVersion returns the launcher build version (injected via -ldflags in release builds).
func (a *App) GetAppVersion() string {
	return strings.TrimSpace(appVersion)
}

// CheckForUpdates fetches the latest GitHub release and compares it to the embedded version.
// Network or parse errors are swallowed; the UI only shows a prompt when UpdateAvailable is true.
func (a *App) CheckForUpdates() UpdateCheckResult {
	res := UpdateCheckResult{CurrentVersion: strings.TrimSpace(appVersion)}

	cur, ok := normalizeSemverTag(res.CurrentVersion)
	if !ok {
		res.Skipped = true
		return res
	}

	tag, pageURL, err := fetchLatestGitHubRelease(res.CurrentVersion)
	if err != nil {
		res.CheckFailed = true
		return res
	}
	res.LatestVersion = tag
	res.ReleaseURL = pageURL

	latest, ok := normalizeSemverTag(tag)
	if !ok {
		res.Skipped = true
		return res
	}

	if semver.Compare(latest, cur) > 0 {
		res.UpdateAvailable = true
	} else {
		res.UpToDate = true
	}
	return res
}

// OpenReleasePage opens a release URL in the system browser.
func (a *App) OpenReleasePage(url string) error {
	u := strings.TrimSpace(url)
	if u == "" || !strings.HasPrefix(u, "https://github.com/") {
		return fmt.Errorf("invalid release URL")
	}
	return openURL(u)
}

func normalizeSemverTag(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "dev") {
		return "", false
	}
	if !strings.HasPrefix(s, "v") {
		s = "v" + s
	}
	if semver.IsValid(s) {
		return s, true
	}
	return "", false
}

func fetchLatestGitHubRelease(userAgentVersion string) (tagName, htmlURL string, err error) {
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest(http.MethodGet, githubLatestReleaseAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	ua := "mtools-launcher"
	if userAgentVersion != "" {
		ua = ua + "/" + userAgentVersion
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github api: %s", resp.Status)
	}

	var rel githubReleasePayload
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return "", "", fmt.Errorf("empty tag_name")
	}
	return strings.TrimSpace(rel.TagName), strings.TrimSpace(rel.HTMLURL), nil
}
