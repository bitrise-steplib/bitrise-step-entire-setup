// Package installer fetches one Entire CLI release and puts its git-remote-entire helper on disk.
package installer

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DefaultReleaseBaseURL is where entireio/cli publishes its release assets.
const DefaultReleaseBaseURL = "https://github.com/entireio/cli/releases/download"

// HelperName is the git remote helper git runs for entire:// URLs.
const HelperName = "git-remote-entire"

// ChecksumsFile lists the sha256 of every asset of a release.
const ChecksumsFile = "checksums.txt"

// Release addresses the assets of one entireio/cli version.
type Release struct {
	Version string
	BaseURL string
}

// AssetName is the archive entireio/cli publishes for a platform.
func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux":
	default:
		return "", fmt.Errorf("Entire publishes no %s build of %s", goos, HelperName)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("Entire publishes no %s/%s build of %s", goos, goarch, HelperName)
	}
	return fmt.Sprintf("entire_%s_%s.tar.gz", goos, goarch), nil
}

// AssetURL is the download URL of one asset of the release.
func (r Release) AssetURL(asset string) string {
	base := r.BaseURL
	if base == "" {
		base = DefaultReleaseBaseURL
	}
	return fmt.Sprintf("%s/v%s/%s", strings.TrimSuffix(base, "/"), r.Version, asset)
}

// Download writes the body of url to dest.
func Download(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		return fmt.Errorf("download %s: %w", url, err)
	}
	return file.Close()
}

// ExpectedChecksum finds the sha256 of asset in a checksums.txt body ("<hex>  <name>" lines).
func ExpectedChecksum(checksums, asset string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s has no entry for %s", ChecksumsFile, asset)
}

// VerifyChecksum fails unless the file's sha256 is expectedHex.
func VerifyChecksum(path, expectedHex string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != strings.ToLower(expectedHex) {
		return fmt.Errorf("checksum of %s is %s, expected %s", filepath.Base(path), actual, expectedHex)
	}
	return nil
}

// ExtractHelper writes only the HelperName member of the tar.gz at archivePath into installDir
// and returns the helper's path. Every other member, including anything that would escape
// installDir, is ignored.
func ExtractHelper(archivePath, installDir string) (string, error) {
	archive, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = archive.Close() }()

	unzipped, err := gzip.NewReader(archive)
	if err != nil {
		return "", fmt.Errorf("%s is not a gzip archive: %w", filepath.Base(archivePath), err)
	}
	defer func() { _ = unzipped.Close() }()

	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(installDir, HelperName)

	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s has no %s", filepath.Base(archivePath), HelperName)
		}
		if err != nil {
			return "", fmt.Errorf("read %s: %w", filepath.Base(archivePath), err)
		}
		// Entire's archives put the binaries at the root; a nested or traversing path is not ours.
		if header.Typeflag != tar.TypeReg || header.Name != HelperName {
			continue
		}

		if err := writeFile(dest, reader); err != nil {
			return "", err
		}
		return dest, nil
	}
}

func writeFile(dest string, content io.Reader) error {
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", dest, err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	// The archive's mode is ignored on purpose: the helper must be runnable whatever it says.
	return os.Chmod(dest, 0o755)
}
