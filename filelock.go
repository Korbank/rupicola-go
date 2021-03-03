package rupicola

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
)

// FileLock guard
type UnixListenerWithLock struct {
	net.Listener
	file *os.File
}

// Close listener and remove lock
func (f *UnixListenerWithLock) Close() error {
	if f == nil {
		return nil
	}
	if f.file == nil {
		return nil
	}
	errLis := f.Listener.Close()
	err := os.Remove(f.file.Name())
	err2 := f.file.Close()
	f.file = nil
	if errLis != nil {
		return errLis
	}
	if err != nil {
		return err
	}
	if err2 != nil {
		return err2
	}
	return nil
}

// ListenUnixLock is like net.ListenUnix but with lock
func ListenUnixLock(addr string) (net.Listener, error) {
	var lockFile *os.File
	if !strings.HasPrefix(addr, "@") {
		// skip lock on abstract socket
		var err error
		// so... android one is funy little beast where issuing stop command to service
		// will kill it in no time (SIGKILL) - thats mean leftovers will stay there forever
		lockDirPath, lockFilePath := path.Split(path.Clean(addr))
		// now prepend lockFilePath by ~lock.
		lockFilePath = path.Join(lockDirPath, "~lock."+lockFilePath)

		lockFile, err = os.Create(lockFilePath)

		if err != nil {
			return nil, fmt.Errorf("failed create lock file %s: %w", lockFilePath, err)
		}

		flock := &syscall.Flock_t{
			Len:    0,
			Start:  0,
			Type:   syscall.F_WRLCK,
			Whence: io.SeekStart,
		}
		if err := syscall.FcntlFlock(lockFile.Fd(), syscall.F_SETLK, flock); err != nil {
			return nil, fmt.Errorf("failed geting exclusive lock %s: %w", lockFilePath, err)
		}

		if _, err := io.WriteString(lockFile, strconv.Itoa(os.Getpid())); err != nil {
			log.Println("management: unable to write PID to lockfile - this is not fatal but weird")
		}

		if _, err := os.Stat(addr); err == nil || !os.IsNotExist(err) {
			// so path exists or we cannot determine it
			log.Printf("management: leftovers from previous session (?) removing socket '%s', as we hold the lock\n", addr)
			if err := os.Remove(addr); err != nil {
				return nil, fmt.Errorf("socket leftovers are still there %s: %v", addr, err)
			}
		}
	}
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	return &UnixListenerWithLock{
		file:     lockFile,
		Listener: l,
	}, nil
}
