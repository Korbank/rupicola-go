package rupicola

import (
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/korbank/rupicola-go/config"
)

type Bind config.Bind

// Bind to interface and Start listening using provided mux and limits
func (bind *Bind) Bind(mux *http.ServeMux, limits config.Limits) error {
	srv := &http.Server{
		Addr:        bind.Address + ":" + strconv.Itoa(int(bind.Port)),
		Handler:     mux,
		ReadTimeout: limits.ReadTimeout,
		IdleTimeout: limits.ReadTimeout,
	}

	switch bind.Type {
	case config.HTTP:
		Logger.Info().Str("type", "http").Str("address", bind.Address).Uint16("port", bind.Port).Msg("starting listener")
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
		Logger.Info().Str("type", "https").Str("address", bind.Address).Uint16("port", bind.Port).Msg("starting listener")
		ln, err := ListenKeepAlive("tcp", srv.Addr)
		if err != nil {
			return err
		}
		return srv.ServeTLS(ln, bind.Cert, bind.Key)

	case config.Unix:
		//todo: check
		Logger.Info().Str("type", "unix").Str("address", bind.Address).Msg("starting listener")
		srv.Addr = bind.Address
		// Change umask to ensure socker is created with right
		// permissions (at this point no other IO opeations are running)
		// and then restore previous umask
		oldmask := myUmask(int(bind.Mode) ^ 0777)
		ln, err := ListenUnixLock(bind.Address)
		myUmask(oldmask)

		if err != nil {
			return err
		}

		defer ln.Close()
		uid := os.Getuid()
		gid := os.Getgid()
		if bind.UID >= 0 {
			uid = bind.UID
		}
		if bind.GID >= 0 {
			gid = bind.GID
		}
		if err := os.Chown(bind.Address, uid, gid); err != nil {
			Logger.Error().Str("address", bind.Address).Int("uid", uid).Int("gid", gid).Msg("Setting permission failed")
			return err
		}
		return srv.Serve(ln)
	case config.BindTypeUnknown:
		fallthrough
	default:
		return errors.New("unsupported bind type")
		// return errors.New("unknown bind type")
	}
}
