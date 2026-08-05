package cmd

import (
	"os"

	"cmaker/internal/cmake"
	"cmaker/internal/config"
)

// loadConfigOrExit reads and validates cmaker.yaml in the current
// directory, exiting the process on any failure - the CLI-facing wrapper
// around the pure config.Load, which itself never exits or prints.
func loadConfigOrExit() config.Config {
	cfg, err := config.Load("cmaker.yaml")
	if err != nil {
		errorf("%v", err)
		os.Exit(1)
	}
	debugf("loaded config: %+v", cfg)
	return cfg
}

// syncConfig loads cmaker.yaml (see loadConfigOrExit) and regenerates
// CMakeLists.txt to match. It exits the process on any failure.
func syncConfig() config.Config {
	cfg := loadConfigOrExit()
	if err := cmake.Generate(".", cfg); err != nil {
		errorf("failed to write CMakeLists.txt: %v", err)
		os.Exit(1)
	}
	return cfg
}
