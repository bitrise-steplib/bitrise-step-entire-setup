package main

import (
	"os"

	"github.com/bitrise-io/go-steputils/v2/export"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/bitrise-steplib/step-entire-setup/step"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := log.NewLogger()
	envRepository := env.NewRepository()
	exporter := export.NewDefaultExporter(command.NewFactory(envRepository))
	httpClient := retryhttp.NewClient(logger).StandardClient()

	setup := step.New(stepconf.NewInputParser(envRepository), envRepository, &exporter, logger, httpClient)

	config, err := setup.ProcessConfig()
	if err != nil {
		logger.Errorf("%s", err)
		return 1
	}

	result, err := setup.Run(config)
	if err != nil {
		logger.Errorf("%s", err)
		return 1
	}

	if err := setup.Export(result); err != nil {
		logger.Errorf("%s", err)
		return 1
	}

	return 0
}
