// Package step installs git-remote-entire so later steps can clone entire:// repositories.
package step

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-steplib/step-entire-setup/installer"
)

// TokenEnv is the variable git-remote-entire reads its checkout token from. Bitrise sets it for
// builds of Entire projects; the step only checks that it is there.
const TokenEnv = "ENTIRE_TOKEN"

// HelperPathOutput is exported with the installed helper's path.
const HelperPathOutput = "ENTIRE_SETUP_HELPER_PATH"

// Config is the step's parsed input.
type Config struct {
	EntireVersion string          `env:"entire_version,required"`
	InstallDir    string          `env:"install_dir,required"`
	EntireToken   stepconf.Secret `env:"entire_token"`
	Verbose       bool            `env:"verbose,opt[true,false]"`
}

// Result is what Run leaves behind for Export.
type Result struct {
	HelperPath string
	InstallDir string
}

// OutputExporter is the part of go-steputils' exporter the step uses (envman add).
type OutputExporter interface {
	ExportOutput(key, value string) error
}

// Step wires the inputs, the environment and the exporter together.
type Step struct {
	inputParser    stepconf.InputParser
	envRepository  env.Repository
	exporter       OutputExporter
	logger         log.Logger
	httpClient     *http.Client
	releaseBaseURL string
}

// New builds a Step that downloads from Entire's GitHub releases.
func New(inputParser stepconf.InputParser, envRepository env.Repository, exporter OutputExporter, logger log.Logger, httpClient *http.Client) Step {
	return Step{
		inputParser:    inputParser,
		envRepository:  envRepository,
		exporter:       exporter,
		logger:         logger,
		httpClient:     httpClient,
		releaseBaseURL: installer.DefaultReleaseBaseURL,
	}
}

// ProcessConfig parses the inputs and refuses to run without a token, before anything is downloaded.
func (s Step) ProcessConfig() (Config, error) {
	var config Config
	if err := s.inputParser.Parse(&config); err != nil {
		return Config{}, fmt.Errorf("invalid input: %w", err)
	}
	stepconf.Print(config)
	s.logger.EnableDebugLog(config.Verbose)

	if strings.TrimSpace(string(config.EntireToken)) == "" {
		return Config{}, errors.New(TokenEnv + " is not set. Bitrise provides it for the builds of an Entire project when the build starts; " +
			"this step only serves projects whose repository URL starts with entire://")
	}

	if url := s.envRepository.Get("GIT_REPOSITORY_URL"); url != "" && !strings.HasPrefix(url, "entire://") {
		s.logger.Warnf("GIT_REPOSITORY_URL (%s) is not an entire:// URL: the git-clone step will not use the helper this step installs.", url)
	}

	return config, nil
}

// Run downloads the pinned release, verifies it and extracts the helper into the install dir.
func (s Step) Run(config Config) (Result, error) {
	asset, err := installer.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Result{}, err
	}
	release := installer.Release{Version: config.EntireVersion, BaseURL: s.releaseBaseURL}

	tempDir, err := os.MkdirTemp("", "entire-setup")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	s.logger.Infof("Downloading %s %s", asset, release.AssetURL(asset))
	archivePath := filepath.Join(tempDir, asset)
	if err := installer.Download(s.httpClient, release.AssetURL(asset), archivePath); err != nil {
		return Result{}, err
	}
	checksumsPath := filepath.Join(tempDir, installer.ChecksumsFile)
	if err := installer.Download(s.httpClient, release.AssetURL(installer.ChecksumsFile), checksumsPath); err != nil {
		return Result{}, err
	}

	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return Result{}, err
	}
	expected, err := installer.ExpectedChecksum(string(checksums), asset)
	if err != nil {
		return Result{}, err
	}
	if err := installer.VerifyChecksum(archivePath, expected); err != nil {
		return Result{}, err
	}
	s.logger.Debugf("Checksum of %s verified", asset)

	helperPath, err := installer.ExtractHelper(archivePath, config.InstallDir)
	if err != nil {
		return Result{}, err
	}
	s.logger.Donef("%s %s installed to %s", installer.HelperName, config.EntireVersion, helperPath)

	// If a stack ever ships git with a tightened protocol.allow, this is where
	// `git config --global protocol.entire.allow always` would go.
	return Result{HelperPath: helperPath, InstallDir: config.InstallDir}, nil
}

// Export puts the install dir on PATH for the following steps and publishes the helper's path.
func (s Step) Export(result Result) error {
	path := s.envRepository.Get("PATH")
	if !onPath(path, result.InstallDir) {
		if err := s.exporter.ExportOutput("PATH", result.InstallDir+string(os.PathListSeparator)+path); err != nil {
			return fmt.Errorf("export PATH: %w", err)
		}
		s.logger.Printf("%s prepended to PATH", result.InstallDir)
	}
	if err := s.exporter.ExportOutput(HelperPathOutput, result.HelperPath); err != nil {
		return fmt.Errorf("export %s: %w", HelperPathOutput, err)
	}
	return nil
}

func onPath(path, dir string) bool {
	for _, entry := range filepath.SplitList(path) {
		if entry != "" && filepath.Clean(entry) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
