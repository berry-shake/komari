package admin

import "testing"

func TestSelectThemeArchiveSkipsChecksums(t *testing.T) {
	archive := "https://github.com/berry-shake/komari-web/releases/download/1.2.4/dist-release.zip"
	got, err := selectThemeArchive([]string{archive + ".sha256", "https://example.com/SHA256SUMS", archive})
	if err != nil || got != archive {
		t.Fatalf("archive = %q, err = %v", got, err)
	}
	if _, err := selectThemeArchive([]string{archive + ".sha256"}); err == nil {
		t.Fatal("accepted checksum as theme")
	}
}
