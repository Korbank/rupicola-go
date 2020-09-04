package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/korbank/rupicola-go"

	log "github.com/rs/zerolog"
)

var logger = log.New(os.Stderr).Level(log.TraceLevel).With().Timestamp().Logger()

func registerCleanupAtExit(config *rupicola.Config) {
	sigc := make(chan os.Signal, 10)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
	go func(c chan os.Signal) {
		// todo: configuration reloading
		// Wait for a SIGINT or SIGKILL:
		for {
			sig := <-c
			logger.Info().Str("signal", sig.String()).Msg("Caught signal: shutting down.")

			switch sig {
			case syscall.SIGHUP:
				logger.Warn().Msg("TODO: reloading config, please restart process")
			default:
				// Stop listening (and unlink the socket if unix type):
				for _, bind := range config.Protocol.Bind {
					if bind.Type != rupicola.Unix {
						continue
					}
					if err := os.Remove(bind.Address); err != nil {
						logger.Error().Str("address", bind.Address).Err(err).Msg("Unable to unlink")
					} else {
						logger.Debug().Str("address", bind.Address).Msg("Unlinked UNIX address")
					}
				}
				// And we're done:
				os.Exit(0)
			}
		}
	}(sigc)
}

func main() {
	configPath := flag.String("config", "", "Specify directory or config file")
	flag.Parse()
	if *configPath == "" {
		flag.Usage()
		os.Exit(1)
	}
	rupicola.Logger = logger
	configuration, err := rupicola.ReadConfig(*configPath)

	if err != nil {
		logger.Error().Err(err).Msg("Unable to parse config")
		os.Exit(1)
	}

	if len(configuration.Methods) == 0 {
		logger.Error().Msg("No method defined in config")
		os.Exit(1)
	}

	if len(configuration.Protocol.Bind) == 0 {
		logger.Error().Msg("No valid bind points")
		os.Exit(1)
	}

	configuration.SetLogging()

	registerCleanupAtExit(configuration)
	err = rupicola.ListenAndServe(configuration)
	logger.Error().Err(err).Msg("Program will shut down now due to encountered error")
	os.Exit(1)
}
