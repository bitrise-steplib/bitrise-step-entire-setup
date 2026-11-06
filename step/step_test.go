package step

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-steplib/step-entire-setup/installer"
)

type fakeEnv map[string]string

func (e fakeEnv) Get(key string) string       { return e[key] }
func (e fakeEnv) Set(key, value string) error { e[key] = value; return nil }
func (e fakeEnv) Unset(key string) error      { delete(e, key); return nil }
func (e fakeEnv) List() []string {
	var list []string
	for k, v := range e {
		list = append(list, k+"="+v)
	}
	return list
}

type recordingLogger struct {
	log.Logger
	warnings []string
}

func (l *recordingLogger) Warnf(format string, v ...interface{}) {
	l.warnings = append(l.warnings, fmt.Sprintf(format, v...))
}

type fakeExporter map[string]string

func (e fakeExporter) ExportOutput(key, value string) error { e[key] = value; return nil }

func inputs(overrides map[string]string) fakeEnv {
	e := fakeEnv{"entire_version": "0.12.2", "install_dir": "/tmp/bin", "entire_token": "token", "verbose": "false"}
	for k, v := range overrides {
		e[k] = v
	}
	return e
}

func newStep(e fakeEnv, logger log.Logger, client *http.Client, baseURL string) Step {
	s := New(stepconf.NewInputParser(e), e, fakeExporter{}, logger, client)
	s.releaseBaseURL = baseURL
	return s
}

func TestProcessConfigRefusesABlankToken(t *testing.T) {
	for _, token := range []string{"", "  "} {
		e := inputs(map[string]string{"entire_token": token})
		_, err := newStep(e, log.NewLogger(), nil, "").ProcessConfig()
		if err == nil || !strings.Contains(err.Error(), TokenEnv) {
			t.Errorf("token %q: err = %v", token, err)
		}
	}
}

func TestProcessConfigWarnsAboutANonEntireURL(t *testing.T) {
	logger := &recordingLogger{Logger: log.NewLogger()}
	e := inputs(map[string]string{"GIT_REPOSITORY_URL": "https://github.com/acme/api.git"})

	if _, err := newStep(e, logger, nil, "").ProcessConfig(); err != nil {
		t.Fatal(err)
	}
	if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], "not an entire:// URL") {
		t.Errorf("warnings = %q", logger.warnings)
	}

	logger = &recordingLogger{Logger: log.NewLogger()}
	e = inputs(map[string]string{"GIT_REPOSITORY_URL": "entire://aws-us-east-2.entire.io/et/acme/api"})
	if _, err := newStep(e, logger, nil, "").ProcessConfig(); err != nil || len(logger.warnings) != 0 {
		t.Errorf("entire URL: err = %v, warnings = %q", err, logger.warnings)
	}
}

func TestRunInstallsTheVerifiedHelper(t *testing.T) {
	asset, err := installer.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	archive := archiveWith(t, map[string]string{"entire": "cli", installer.HelperName: "helper"})
	sum := sha256.Sum256(archive)
	checksums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.12.2/" + asset:
			_, _ = w.Write(archive)
		case "/v0.12.2/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	installDir := filepath.Join(t.TempDir(), "bin")

	result, err := newStep(inputs(nil), log.NewLogger(), server.Client(), server.URL).Run(Config{EntireVersion: "0.12.2", InstallDir: installDir})
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(result.HelperPath); string(content) != "helper" {
		t.Errorf("installed %q at %s", content, result.HelperPath)
	}
}

func TestRunRejectsAWrongChecksum(t *testing.T) {
	asset, err := installer.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	archive := archiveWith(t, map[string]string{installer.HelperName: "helper"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + asset + "\n"))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	installDir := filepath.Join(t.TempDir(), "bin")

	_, err = newStep(inputs(nil), log.NewLogger(), server.Client(), server.URL).Run(Config{EntireVersion: "0.12.2", InstallDir: installDir})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, installer.HelperName)); statErr == nil {
		t.Error("the helper was installed despite the checksum mismatch")
	}
}

func TestExportPrependsThePathOnce(t *testing.T) {
	exporter := fakeExporter{}
	e := fakeEnv{"PATH": "/usr/bin:/bin"}
	s := New(stepconf.NewInputParser(e), e, exporter, log.NewLogger(), nil)

	if err := s.Export(Result{HelperPath: "/opt/entire/git-remote-entire", InstallDir: "/opt/entire"}); err != nil {
		t.Fatal(err)
	}
	if exporter["PATH"] != "/opt/entire:/usr/bin:/bin" || exporter[HelperPathOutput] != "/opt/entire/git-remote-entire" {
		t.Errorf("exported %v", exporter)
	}

	exporter = fakeExporter{}
	e = fakeEnv{"PATH": "/usr/bin:/opt/entire/:/bin"}
	s = New(stepconf.NewInputParser(e), e, exporter, log.NewLogger(), nil)
	if err := s.Export(Result{HelperPath: "/opt/entire/git-remote-entire", InstallDir: "/opt/entire"}); err != nil {
		t.Fatal(err)
	}
	if _, exported := exporter["PATH"]; exported {
		t.Errorf("PATH exported again although the install dir is already on it: %v", exporter)
	}
}

func archiveWith(t *testing.T, members map[string]string) []byte {
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
	return buf.Bytes()
}
