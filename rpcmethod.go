package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os/exec"
	"time"

	"github.com/mkocot/rupicolarpc"

	log "github.com/inconshreveable/log15"
)

func (m *MethodDef) prepareCommand(ctx context.Context, req rupicolarpc.JsonRpcRequest) (*rupicolaRPCContext, *exec.Cmd, error) {
	uncastedContext := req.UserData()
	var ok bool
	var castedContext *rupicolaRPCContext
	if uncastedContext != nil {
		castedContext, ok = uncastedContext.(*rupicolaRPCContext)
	}
	if ok {
		if !castedContext.isAuthorized && castedContext.allowPrivate && m.Private {
			castedContext.shouldRequestAuth = true
			log.Warn("Unauthorized")
			return nil, nil, rpcUnauthorizedError
		}
	} else {
		log.Crit("Provided context is not pointer")
		return nil, nil, rupicolarpc.NewStandardError(rupicolarpc.InternalError)
	}

	if err := m.CheckParams(req); err != nil {
		return nil, nil, err
	}

	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, len(m.InvokeInfo.Args))
	for _, arg := range m.InvokeInfo.Args {
		skip, err := arg.evalueateArgs(req.Params(), buffer)
		if err != nil {
			log.Error("error", "err", err)
			return nil, nil, err
		}
		if !skip {
			appArguments = append(appArguments, buffer.String())
		}
		buffer.Reset()
	}

	m.logger.Debug("prepared method invocation", "exec", m.InvokeInfo.Exec, "args", appArguments)
	process := exec.CommandContext(ctx, m.InvokeInfo.Exec, appArguments...)

	// Make it "better"
	SetUserGroup(process, m)

	stdin, err := process.StdinPipe()
	if err == nil {
		stdin.Close()
	} else {
		m.logger.Error("stdin", "error", err)
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
				m.logger.Warn("error from recovery", "error", err)
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
		m.logger.Error("unable to start process", "err", err)
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
		m.logger.Debug("read loop started")

		_, err := io.Copy(writer, stdout)
		if err != nil {
			if err != io.EOF {
				m.logger.Error("error reading from pipe", "err", err)
				if err := process.Process.Kill(); err != nil {
					m.logger.Error("sending kill failed", "err", err)
				}
			} else {
				m.logger.Debug("reading from pipe finished")
			}
			pw.CloseWithError(err)
		}

		m.logger.Debug("Waiting for clean exit")
		if err := process.Wait(); err != nil {
			m.logger.Error("Waiting for close process failed", "err", err)
		} else {
			m.logger.Debug("Done")
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
