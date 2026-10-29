// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siyuan-note/siyuan/kernel/conf"
)

func TestGetUpdatePackageURLs(t *testing.T) {
	const assetURL = "https://github.com/ulquiola/siyuan/releases/download/v3.8.9/siyuan-3.8.9-win.exe"
	if got := getUpdatePackageURLs(assetURL); !slices.Equal(got, []string{assetURL}) {
		t.Fatalf("got %v, want [%s]", got, assetURL)
	}
}

// Beta 和 Alpha 通道同样使用全部下载源，保证并发探测对三个通道一致生效。
func TestGitHubUpdateReleaseUsesAllSourcesForBetaAndAlpha(t *testing.T) {
	suffix := currentInstallPackageSuffix()
	if "" == suffix {
		t.Skip("automatic installation is desktop-only")
	}
	const checksum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// 两个通道使用不同基础版本，保证各自选中自己的 Release
	tests := map[string]string{
		conf.UpdateChannelBeta:  "v99.0.0-beta.2",
		conf.UpdateChannelAlpha: "v99.1.0-alpha.5",
	}
	releases := make([]*githubRelease, 0, len(tests))
	for _, tag := range tests {
		version := strings.TrimPrefix(tag, "v")
		pkgName := "siyuan-" + version + "-" + suffix
		releases = append(releases, &githubRelease{TagName: tag, HTMLURL: "https://example.invalid/" + version,
			Assets: []*githubReleaseAsset{{
				Name: pkgName, BrowserDownloadURL: "https://github.com/siyuan-note/siyuan/releases/download/" + tag + "/" + pkgName,
				Digest: "sha256:" + checksum, State: "uploaded",
			}}})
	}
	githubReleasesLock.Lock()
	originalReleases, originalCacheTime := cachedGitHubReleases, githubReleasesCacheTime
	cachedGitHubReleases, githubReleasesCacheTime = releases, time.Now().Unix()
	githubReleasesLock.Unlock()
	t.Cleanup(func() {
		githubReleasesLock.Lock()
		cachedGitHubReleases, githubReleasesCacheTime = originalReleases, originalCacheTime
		githubReleasesLock.Unlock()
	})

	for channel, tag := range tests {
		release, err := getGitHubUpdateRelease(channel, false)
		if err != nil {
			t.Fatal(err)
		}
		version := strings.TrimPrefix(tag, "v")
		pkgName := "siyuan-" + version + "-" + suffix
		pkg := release.Packages[pkgName]
		if nil == pkg {
			t.Fatalf("channel [%s] has no package [%s]", channel, pkgName)
		}
		want := []string{
			"https://release.b3log.org/siyuan/" + pkgName,
			"https://release.liuyun.io/siyuan/" + pkgName,
			"https://github.com/siyuan-note/siyuan/releases/download/" + tag + "/" + pkgName,
		}
		if !slices.Equal(pkg.URLs, want) {
			t.Fatalf("channel [%s]: got %v, want %v", channel, pkg.URLs, want)
		}
	}
}

func TestIsReleaseAllowed(t *testing.T) {
	tests := []struct {
		channel string
		version string
		want    bool
	}{
		{conf.UpdateChannelStable, "v3.7.3", true},
		{conf.UpdateChannelStable, "v3.7.4-rc.1", false},
		{conf.UpdateChannelBeta, "v3.7.3", true},
		{conf.UpdateChannelBeta, "v3.7.4-alpha.1", false},
		{conf.UpdateChannelBeta, "v3.7.4-beta.1", true},
		{conf.UpdateChannelBeta, "v2.0.0-beta2", true},
		{conf.UpdateChannelBeta, "v3.7.4-rc.1", true},
		{conf.UpdateChannelBeta, "v1.2.0-rc3", true},
		{conf.UpdateChannelAlpha, "v3.7.4-alpha.1", true},
		{conf.UpdateChannelAlpha, "v0.5.6-alpha1", true},
		{conf.UpdateChannelAlpha, "v3.7.4-beta.1", true},
		{conf.UpdateChannelAlpha, "v3.7.4-rc.1", true},
		{conf.UpdateChannelAlpha, "v3.7.4-preview.1", false},
		{conf.UpdateChannelAlpha, "v3.7.4-alphabet.1", false},
		{conf.UpdateChannelAlpha, "invalid", false},
		{"invalid", "v3.7.3", false},
	}
	for _, test := range tests {
		if got := isReleaseAllowed(test.channel, test.version); got != test.want {
			t.Fatalf("channel [%s] version [%s]: got %t, want %t", test.channel, test.version, got, test.want)
		}
	}
}

func TestSelectGitHubRelease(t *testing.T) {
	releases := []*githubRelease{
		{TagName: "v3.7.4-beta.1"},
		{TagName: "v3.7.3"},
		{TagName: "v3.8.0-preview.1"},
		{TagName: "v3.7.4-rc.1"},
		{TagName: "v3.8.0-alpha.1"},
		{TagName: "v4.0.0", Draft: true},
	}

	beta := selectGitHubRelease(releases, conf.UpdateChannelBeta)
	if nil == beta || "v3.7.4-rc.1" != beta.TagName {
		t.Fatalf("unexpected Beta release: %#v", beta)
	}
	alpha := selectGitHubRelease(releases, conf.UpdateChannelAlpha)
	if nil == alpha || "v3.8.0-alpha.1" != alpha.TagName {
		t.Fatalf("unexpected Alpha release: %#v", alpha)
	}
}

func TestParseChecksumManifest(t *testing.T) {
	checksum := strings.Repeat("A", 64)
	manifest := checksum + " *nested\\siyuan-3.7.4-alpha.1-win.exe\n"
	got := parseChecksumManifest(manifest, "siyuan-3.7.4-alpha.1-win.exe")
	if strings.ToLower(checksum) != got {
		t.Fatalf("unexpected checksum: %q", got)
	}
	if "" != parseChecksumManifest(manifest, "siyuan-3.7.4-alpha.1-win-arm64.exe") {
		t.Fatal("unexpected checksum for missing package")
	}
}

func TestNormalizeSHA256(t *testing.T) {
	checksum := strings.Repeat("A", 64)
	if got := normalizeSHA256("sha256:" + checksum); strings.ToLower(checksum) != got {
		t.Fatalf("unexpected normalized checksum: %q", got)
	}
	if "" != normalizeSHA256(strings.Repeat("z", 64)) {
		t.Fatal("invalid checksum should be rejected")
	}
}

func TestSHA256Hash(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "package")
	if err := os.WriteFile(filePath, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := sha256Hash(filePath)
	if err != nil {
		t.Fatal(err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if want != got {
		t.Fatalf("unexpected checksum: %q", got)
	}
}
