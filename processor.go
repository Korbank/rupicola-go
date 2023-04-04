package rupicola

import (
	"net/http"

	"github.com/korbank/rupicola-go/config"
	"github.com/korbank/rupicola-go/rupicolarpc"
)

type rupicolaProcessor struct {
	limits    config.Limits
	processor rupicolarpc.JSONRPCProcessor
	config    Config
}

func newRupicolaProcessorFromConfig(conf Config) *rupicolaProcessor {
	rupicolaProcessor := &rupicolaProcessor{
		config:    conf,
		limits:    conf.Limits,
		processor: rupicolarpc.NewJSONRPCProcessor(),
	}

	for methodName := range conf.Methods {
		methodDef := conf.Methods[methodName]

		var metypes []rupicolarpc.MethodType

		switch {
		case methodDef.AllowStreamed && methodDef.AllowRPC:
			metypes = []rupicolarpc.MethodType{
				rupicolarpc.StreamingMethodLegacy,
				rupicolarpc.RPCMethod,
			}
		case methodDef.AllowStreamed:
			metypes = []rupicolarpc.MethodType{
				rupicolarpc.StreamingMethodLegacy,
			}
		case methodDef.AllowRPC:
			metypes = []rupicolarpc.MethodType{
				rupicolarpc.RPCMethod,
			}
		default:
			panic("invalid configuration")
		}

		for i := range metypes {
			metype := metypes[i]
			method := rupicolaProcessor.processor.AddMethod(methodName, metype, methodDef)

			if methodDef.Limits.ExecTimeout >= 0 {
				method.ExecutionTimeout(methodDef.Limits.ExecTimeout)
			}
			if methodDef.Limits.MaxResponse >= 0 {
				method.MaxSize(uint(methodDef.Limits.MaxResponse))
			}
		}
	}
	return rupicolaProcessor
}

// Create separate context for given bind point (required for concurrent listening)
func (proc *rupicolaProcessor) spawnChild(bind config.Bind) *rupicolaProcessorChild {
	child := &rupicolaProcessorChild{
		parent: proc,
		bind:   Bind(bind),
		mux:    http.NewServeMux(),
		log:    Logger.With().Str("bindpoint", bind.Address).Logger(),
	}
	child.mux.Handle(proc.config.Protocol.URI.RPC, child)
	child.mux.Handle(proc.config.Protocol.URI.Streamed, child)
	return child
}

// Start listening (this exits only on failure)
func (child *rupicolaProcessorChild) listen() error {
	return child.bind.Bind(child.mux, child.parent.config.Limits)
}
