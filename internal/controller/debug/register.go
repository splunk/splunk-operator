package debug

import (
	"net/http"
	"net/http/pprof"
)

// Options used to provide configuration options
type Options struct {
	CmdLine      bool
	Profile      bool
	Symbol       bool
	Trace        bool
	Heap         bool
	Block        bool
	Goroutine    bool
	Threadcreate bool
}

// DefaultOptions returns default options configuration
func DefaultOptions() *Options {
	return &Options{
		CmdLine:      true,
		Profile:      true,
		Symbol:       true,
		Trace:        true,
		Heap:         true,
		Block:        true,
		Goroutine:    true,
		Threadcreate: true,
	}
}

// RegisterEndpoint used to register the different debug endpoints
func RegisterEndpoint(register func(string, http.Handler) error, options *Options) error {
	err := register("/debug/pprof", http.HandlerFunc(pprof.Index))
	if err != nil {
		return err
	}

	if options == nil {
		options = DefaultOptions()
	}
	if options.CmdLine {
		err := register("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
		if err != nil {
			return err
		}
	}
	if options.Profile {
		err := register("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
		if err != nil {
			return err
		}
	}
	if options.Symbol {
		err := register("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
		if err != nil {
			return err
		}
	}
	if options.Trace {
		err := register("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
		if err != nil {
			return err
		}
	}
	if options.Heap {
		err := register("/debug/pprof/heap", pprof.Handler("heap"))
		if err != nil {
			return err
		}
	}
	if options.Block {
		err := register("/debug/pprof/block", pprof.Handler("block"))
		if err != nil {
			return err
		}
	}
	if options.Goroutine {
		err := register("/debug/pprof/goroutine", pprof.Handler("goroutine"))
		if err != nil {
			return err
		}
	}
	if options.Threadcreate {
		err := register("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
		if err != nil {
			return err
		}
	}

	return nil
}
