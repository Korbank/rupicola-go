package rupicola

import (
	"net/http"

	"github.com/korbank/rupicola-go/config"
	"github.com/korbank/rupicola-go/rupicolarpc"
)

type rupicolaProcessor struct {
	limits    config.Limits
	processor rupicolarpc.JsonRpcProcessor
	config    *Config
}

func newRupicolaProcessorFromConfig(conf *Config) *rupicolaProcessor {
	rupicolaProcessor := &rupicolaProcessor{
		config:    conf,
		limits:    conf.Limits,
		processor: rupicolarpc.NewJsonRpcProcessor()}

	for k, v := range conf.Methods {
		var metype rupicolarpc.MethodType
		if v.Streamed {
			metype = rupicolarpc.StreamingMethodLegacy
		} else {
			metype = rupicolarpc.RPCMethod
		}

		method := rupicolaProcessor.processor.AddMethod(k, metype, v)

		if v.Limits.ExecTimeout >= 0 {
			method.ExecutionTimeout(v.Limits.ExecTimeout)
		}
		if v.Limits.MaxResponse >= 0 {
			method.MaxSize(uint(v.Limits.MaxResponse))
		}
	}
	return rupicolaProcessor
}

// Create separate context for given bind point (required for concurrent listening)
func (proc *rupicolaProcessor) spawnChild(bind *config.Bind) *rupicolaProcessorChild {
	child := &rupicolaProcessorChild{proc, (*Bind2)(bind), http.NewServeMux(), Logger.With().Str("bindpoint", bind.Address).Logger()}
	child.mux.Handle(proc.config.Protocol.URI.RPC, child)
	child.mux.Handle(proc.config.Protocol.URI.Streamed, child)
	return child
}

// Start listening (this exits only on failure)
func (child *rupicolaProcessorChild) listen() error {
	return child.bind.Bind(child.mux, child.parent.config.Limits)
}
