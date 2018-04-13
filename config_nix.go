// +build !windows

package main

import "log/syslog"
import log "github.com/inconshreveable/log15"

func configureSyslog(path string) (string, error) {
	return log.SyslogNetHandler("", path, syslog.LOG_DAEMON, "rupicola", log.JsonFormat())
}
