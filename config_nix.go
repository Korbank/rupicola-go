// +build !windows

package rupicola

import "log/syslog"
import log "github.com/inconshreveable/log15"

func configureSyslog(path string) (log.Handler, error) {
	return log.SyslogNetHandler("", path, syslog.LOG_DAEMON, "rupicola", log.JsonFormat())
}
