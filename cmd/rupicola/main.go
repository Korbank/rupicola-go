package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"bitbucket.org/kociolek/rupicola-ng"

	log "github.com/inconshreveable/log15"
)

func registerCleanupAtExit(config *rupicola.RupicolaConfig) {
	sigc := make(chan os.Signal, 10)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
	go func(c chan os.Signal) {
		// todo: configuration reloading
		// Wait for a SIGINT or SIGKILL:
		for {
			sig := <-c
			log.Info("Caught signal: shutting down.", "signal", sig)

			switch sig {
			case syscall.SIGHUP:
				log.Warn("TODO: reloading config, please restart process")
			default:
				// Stop listening (and unlink the socket if unix type):
				for _, bind := range config.Protocol.Bind {
					if bind.Type != rupicola.Unix {
						continue
					}
					if err := os.Remove(bind.Address); err != nil {
						log.Error("Unable to unlink", "address", bind.Address, "err", err)
					} else {
						log.Debug("Unlinked UNIX address", "address", bind.Address)
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

	configuration, err := rupicola.ReadConfig(*configPath)

	if err != nil {
		log.Crit("Unable to parse config", "err", err)
		os.Exit(1)
	}

	if len(configuration.Methods) == 0 {
		log.Crit("No method defined in config")
		os.Exit(1)
	}

	if len(configuration.Protocol.Bind) == 0 {
		log.Crit("No valid bind points")
		os.Exit(1)
	}

	configuration.SetLogging()

	registerCleanupAtExit(configuration)
	err = rupicola.ListenAndServe(configuration)
	log.Crit("Program will shut down now due to encountered error", "error", err)
	os.Exit(1)
}
