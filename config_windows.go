package rupicola

import (
	"errors"

	log "github.com/rs/zerolog"
)

func configureSyslog(path string) (log.SyslogWriter, error) {
	return nil, errors.New("Syslog unsupported on Windows")
}
