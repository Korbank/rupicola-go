// +build !windows

package rupicola

import (
	"log/syslog"

	log "github.com/rs/zerolog"
)

func configureSyslog(path string) (log.SyslogWriter, error) {
	return syslog.Dial("", path, syslog.LOG_DAEMON, "rupicola")
}
