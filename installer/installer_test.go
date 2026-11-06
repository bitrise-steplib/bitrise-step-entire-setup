package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
		fails              bool
	}{
		{"linux", "amd64", "entire_linux_amd64.tar.gz", false},
		{"darwin", "arm64", "entire_darwin_arm64.tar.gz", false},
		{"windows", "amd64", "", true},
		{"linux", "386", "", true},
	}
	for _, c := range cases {
		got, err := AssetName(c.goos, c.goarch)
		if c.fails != (err != nil) || got != c.want {
			t.Errorf("AssetName(%s, %s) = %q, %v", c.goos, c.goarch, got, err)
		}
	}
}

func TestAssetURL(t *testing.T) {
	got := Release{Version: "0.12.2"}.AssetURL("checksums.txt")
	if got != "https://github.com/entireio/cli/releases/download/v0.12.2/checksums.txt" {
		t.Errorf("AssetURL = %q", got)
	}
	got = Release{Version: "1.0.0", BaseURL: "http://localhost:1/"}.AssetURL("a.tar.gz")
	if got != "http://localhost:1/v1.0.0/a.tar.gz" {
		t.Errorf("AssetURL with base = %q", got)
	}
}

func TestExpectedChecksum(t *testing.T) {
	checksums := "aaa  entire_darwin_amd64.tar.gz\nBBB  entire_linux_amd64.tar.gz\nmalformed line\n"

	if got, err := ExpectedChecksum(checksums, "entire_linux_amd64.tar.gz"); err != nil || got != "bbb" {
		t.Errorf("ExpectedChecksum = %q, %v", got, err)
	}
	if _, err := ExpectedChecksum(checksums, "entire_linux_arm64.tar.gz"); err == nil {
		t.Error("ExpectedChecksum found a missing asset")
	}
}

func TestVerifyChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset")
	if err := os.WriteFile(path, []byte("entire"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("entire"))

	if err := VerifyChecksum(path, hex.EncodeToString(sum[:])); err != nil {
		t.Errorf("matching checksum rejected: %v", err)
	}
	if err := VerifyChecksum(path, "00"); err == nil {
		t.Error("wrong checksum accepted")
	}
}

func TestExtractHelper(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "entire.tar.gz")
	writeArchive(t, archive, map[string]string{
		"LICENSE":                 "mit",
		"entire":                  "cli",
		"../" + HelperName:        "escaped",
		"completions/entire.bash": "",
		HelperName:                "helper",
	})
	installDir := filepath.Join(t.TempDir(), "bin")

	got, err := ExtractHelper(archive, installDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(installDir, HelperName) {
		t.Errorf("helper path = %q", got)
	}
	content, err := os.ReadFile(got)
	if err != nil || string(content) != "helper" {
		t.Errorf("helper content = %q, %v", content, err)
	}
	info, err := os.Stat(got)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("helper mode = %v, %v", info.Mode(), err)
	}
	entries, err := os.ReadDir(installDir)
	if err != nil || len(entries) != 1 {
		t.Errorf("install dir holds %d entries, want only the helper", len(entries))
	}
	if _, err := os.Stat(filepath.Join(installDir, "..", HelperName)); err == nil {
		t.Error("a traversing member was written outside the install dir")
	}
}

func TestExtractHelperWithoutHelper(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "entire.tar.gz")
	writeArchive(t, archive, map[string]string{"entire": "cli"})

	if _, err := ExtractHelper(archive, t.TempDir()); err == nil {
		t.Error("an archive without the helper was accepted")
	}
}

func TestDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			_, _ = w.Write([]byte("body"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "file")

	if err := Download(server.Client(), server.URL+"/ok", dest); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(dest); string(content) != "body" {
		t.Errorf("downloaded %q", content)
	}
	if err := Download(server.Client(), server.URL+"/missing", dest); err == nil {
		t.Error("a 404 was accepted")
	}
}

func writeArchive(t *testing.T, path string, members map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range members {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
