package rupicola

import (
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"strconv"
	"github.com/korbank/rupicola-go/config"
)
type Bind2 struct {
	internal config.Bind
}
// Bind to interface and Start listening using provided mux and limits
func (bind *Bind2) Bind(mux *http.ServeMux, limits config.Limits) error {
	srv := &http.Server{
		Addr:        bind.internal.Address + ":" + strconv.Itoa(int(bind.internal.Port)),
		Handler:     mux,
		ReadTimeout: limits.ReadTimeout,
		IdleTimeout: limits.ReadTimeout,
	}

	switch bind.internal.Type {
	case config.HTTP:
		Logger.Info().Str("type", "http").Str("address", bind.internal.Address).Uint16("port", bind.internal.Port).Msg("starting listener")
		ln, err := ListenKeepAlive("tcp", srv.Addr)
		if err != nil {
			return err
		}
		return srv.Serve(ln)

	case config.HTTPS:
		srv.TLSConfig = &tls.Config{
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
		}
		Logger.Info().Str("type", "https").Str("address", bind.internal.Address).Uint16("port", bind.internal.Port).Msg("starting listener")
		ln, err := ListenKeepAlive("tcp", srv.Addr)
		if err != nil {
			return err
		}
		return srv.ServeTLS(ln, bind.internal.Cert, bind.internal.Key)

	case config.Unix:
		//todo: check
		Logger.Info().Str("type", "unix").Str("address", bind.internal.Address).Msg("starting listener")
		srv.Addr = bind.internal.Address
		// Change umask to ensure socker is created with right
		// permissions (at this point no other IO opeations are running)
		// and then restore previous umask
		oldmask := myUmask(int(bind.internal.Mode) ^ 0777)
		ln, err := ListenUnixLock(bind.internal.Address)
		myUmask(oldmask)

		if err != nil {
			return err
		}

		defer ln.Close()
		uid := os.Getuid()
		gid := os.Getgid()
		if bind.internal.UID >= 0 {
			uid = bind.internal.UID
		}
		if bind.internal.GID >= 0 {
			gid = bind.internal.GID
		}
		if err := os.Chown(bind.internal.Address, uid, gid); err != nil {
			Logger.Error().Str("address", bind.internal.Address).Int("uid", uid).Int("gid", gid).Msg("Setting permission failed")
			return err
		}
		return srv.Serve(ln)
	}
	return errors.New("Unknown case")
}
