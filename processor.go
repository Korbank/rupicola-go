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
func (proc *rupicolaProcessor) spawnChild(bind config.Bind) rupicolaProcessorChild {
	child := rupicolaProcessorChild{
		parent: proc,
		bind:   Bind(bind),
		log:    Logger.With().Str("bindpoint", bind.Address).Logger(),
	}
	return child
}

// Start listening (this exits only on failure)
func (child *rupicolaProcessorChild) listen() error {
	mux := http.NewServeMux()
	cfg := child.config()
	mux.Handle(cfg.Protocol.URI.RPC, child)
	mux.Handle(cfg.Protocol.URI.Streamed, child)
	return child.bind.Bind(mux, cfg.Limits)
}
