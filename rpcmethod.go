package rupicola

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os/exec"
	"time"

	"github.com/korbank/rupicola-go/rupicolarpc"
)

func (m *MethodDef) prepareCommand(ctx context.Context, req rupicolarpc.JsonRpcRequest) (*rupicolaRPCContext, *exec.Cmd, error) {
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
	//var additionalParams map[string]interface{}
	//	// Check if required arguments are present
	params := req.Params()
	if params == nil {
		params = make(map[string]interface{})
	}
	if err := m.CheckParams(params); err != nil {
		return nil, nil, err
	}

	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, len(m.InvokeInfo.Args))
	for _, arg := range m.InvokeInfo.Args {
		skip, err := arg.evalueateArgs(params, buffer)
		if err != nil {
			m.logger.Error().Err(err).Msg("error")
			return nil, nil, err
		}
		if !skip {
			appArguments = append(appArguments, buffer.String())
		}
		buffer.Reset()
	}

	m.logger.Debug().Str("exec", m.InvokeInfo.Exec).Strs("args", appArguments).Msg("prepared method invocation")
	process := exec.CommandContext(ctx, m.InvokeInfo.Exec, appArguments...)

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
func (m *MethodDef) Invoke(ctx context.Context, req rupicolarpc.JsonRpcRequest) (interface{}, error) {
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
		//out.SetResponseError(err)
		return nil, err
	}
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

	if m.Encoding == Base64 {
		writerEncoder := base64.NewEncoder(base64.URLEncoding, writer)
		// should we defer, or err check?
		defer writerEncoder.Close()
		writer = writerEncoder
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
