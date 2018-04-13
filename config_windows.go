package main

import "errors"
import log "github.com/inconshreveable/log15"

func configureSyslog(path string) (log.Handler, error) {
	return nil, errors.New("Syslog unsupported on Windows")
}
