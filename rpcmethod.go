package rupicola

import (
	"bytes"
	"context"
	"encoding/ascii85"
	"encoding/base64"
	"io"
	"os/exec"
	"time"

	"github.com/korbank/rupicola-go/config"
	"github.com/korbank/rupicola-go/rupicolarpc"
)

func (m *MethodDef) execParamsLen() int {
	switch m.InvokeInfo.Exec.Mode {
	case config.ExecTypeDefault:
		return len(m.InvokeInfo.Args)
		// case config.ExecTypeShellWrapper:
		// 	// Additional parameters:
		// 	// '-c' and $method_name
		// 	return len(m.InvokeInfo.Args) + len(m.Params) + 2
		// default:
	}
	panic("unknown mode")
}

func (m *MethodDef) prepareCommand(ctx context.Context, req rupicolarpc.JSONRPCRequest) (*rupicolaRPCContext, *exec.Cmd, error) {
	uncastedContext := req.UserData()
	var ok bool
	var castedContext *rupicolaRPCContext
	if uncastedContext != nil {
		castedContext, ok = uncastedContext.(*rupicolaRPCContext)
	}
	if !ok {
		m.logger.Error().Msg("Provided context is not pointer")
		return nil, nil, rupicolarpc.NewStandardError(rupicolarpc.InternalError)
	}
	if !castedContext.isAuthorized && (!castedContext.allowPrivate || !m.Private) {
		castedContext.shouldRequestAuth = true
		m.logger.Warn().Msg("Unauthorized")
		return nil, nil, rpcUnauthorizedError
	}
	// We will create this when needed
	// var additionalParams map[string]interface{}
	//	// Check if required arguments are present
	params := req.Params()
	if params == nil {
		params = make(map[string]interface{})
	}
	if err := m.CheckParams(params); err != nil {
		return nil, nil, err
	}

	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, m.execParamsLen())

	for _, arg := range m.InvokeInfo.Args {
		skip, err := evalueateArgs(arg, params, buffer)
		if err != nil {
			m.logger.Error().Err(err).Msg("error")
			return nil, nil, err
		}
		if !skip {
			appArguments = append(appArguments, buffer.String())
		}
		buffer.Reset()
	}

	var process *exec.Cmd
	switch m.InvokeInfo.Exec.Mode {
	case config.ExecTypeDefault:
	case config.ExecTypeShellWrapper:
		newArguments := make([]string, 0, len(m.Params)+2+len(appArguments))
		newArguments = append(newArguments, "-c")
		if len(appArguments) > 0 {
			appArguments = append(newArguments, appArguments[0])
		}
		appArguments = append(appArguments, req.Method())
		buffer.Reset()
		for name := range m.Params {
			arg := config.MethodArgs{
				Param: name,
			}
			skip, err := evalueateArgs(arg, params, buffer)
			if err != nil {
				m.logger.Error().Err(err).Msg("error")
				return nil, nil, err
			}
			if !skip {
				appArguments = append(appArguments, buffer.String())
			}
			buffer.Reset()
		}

	default:
		panic("should not happend")
	}
	m.logger.Debug().Stringer("exec", m.InvokeInfo.Exec).
		Strs("args", appArguments).Msg("prepared method invocation")
	process = exec.CommandContext(ctx, m.InvokeInfo.Exec.Path, appArguments...)

	// Make it "better"
	SetUserGroup(process, m)

	stdin, err := process.StdinPipe()
	if err == nil {
		stdin.Close()
	} else {
		m.logger.Error().Err(err).Msg("stdin")
		return nil, nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "stdin")
	}

	return castedContext, process, nil
}

// Invoke is implementation of jsonrpc.Invoker
func (m *MethodDef) Invoke(ctx context.Context, req rupicolarpc.JSONRPCRequest) (interface{}, error) {
	defer func() {
		// We don't want close app when we reach panic inside this goroutine
		if r := recover(); r != nil {
			if err, ok := r.(error); ok {
				m.logger.Warn().Err(err).Msg("error from recovery")
			}
		}
	}()

	// We can cancel or set deadline for current context (only shorter - default no limit)
	_, process, err := m.prepareCommand(ctx, req)
	if err != nil {
		// out.SetResponseError(err)
		return nil, err
	}
	m.logger.Debug().Strs("args", process.Args).Msg("process prepared")
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "stdout")
	}
	err = process.Start()
	if err != nil {
		m.logger.Error().Err(err).Msg("unable to start process")
		return nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, err)
	}
	// We also have net.Pipe
	pr, pw := io.Pipe()
	writer := io.Writer(pw)
	reader := io.ReadCloser(pr)

	switch m.Encoding {
	case config.Base64, config.Base64Url:
		var standard *base64.Encoding
		if m.Encoding == config.Base64 {
			standard = base64.StdEncoding
		} else {
			standard = base64.URLEncoding
		}
		writerEncoder := base64.NewEncoder(standard, writer)
		// should we defer, or err check?
		defer writerEncoder.Close()
		writer = writerEncoder
	case config.Base85:
		writerEncoder := ascii85.NewEncoder(writer)
		defer writerEncoder.Close()
		writer = writerEncoder
	case config.Utf8:
		// NOOP
	default:
		panic("unknown encoding")
	}

	go func() {
		time.Sleep(m.InvokeInfo.Delay)
		m.logger.Debug().Msg("read loop started")

		_, err := io.Copy(writer, stdout)
		if err != nil {
			if err != io.EOF {
				m.logger.Error().Err(err).Msg("error reading from pipe")
				if err := process.Process.Kill(); err != nil {
					m.logger.Error().Err(err).Msg("sending kill failed")
				}
			} else {
				m.logger.Debug().Msg("reading from pipe finished")
			}
			pw.CloseWithError(err)
		}

		m.logger.Debug().Msg("Waiting for clean exit")
		if err := process.Wait(); err != nil {
			m.logger.Error().Err(err).Msg("Waiting for close process failed")
			pw.CloseWithError(err)
		} else {
			m.logger.Debug().Msg("Done")
		}
		pw.Close()
	}()

	if m.InvokeInfo.Delay != 0 {
		pw.Close()
		// for "delayed" execution we cannot provide meaningful data
		return "OK", nil
	}
	return reader, nil
}
