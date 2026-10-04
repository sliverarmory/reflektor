package bofloader

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

const maxFormatAllocation = 16 << 20

var executionContexts sync.Map // map[uint64]*executionContext, keyed by native OS thread ID

func registerExecutionContext(context *executionContext) (func(), error) {
	threadID, err := currentExecutionThreadID()
	if err != nil {
		return nil, fmt.Errorf("identify BOF execution thread: %w", err)
	}
	if _, loaded := executionContexts.LoadOrStore(threadID, context); loaded {
		return nil, fmt.Errorf("BOF execution context already active on thread %d", threadID)
	}
	return func() { executionContexts.Delete(threadID) }, nil
}

func activeExecutionContext() *executionContext {
	threadID, err := currentExecutionThreadID()
	if err != nil {
		return nil
	}
	value, ok := executionContexts.Load(threadID)
	if !ok {
		return nil
	}
	return value.(*executionContext)
}

type executionContext struct {
	mu          sync.Mutex
	outputs     []Output
	errors      []error
	allocations map[uintptr][]byte
	emit        func(Output)
	capture     bool
	stop        <-chan struct{}
	stopSeen    atomic.Bool
}

func newExecutionContext(emit func(Output), capture bool) *executionContext {
	return &executionContext{allocations: make(map[uintptr][]byte), emit: emit, capture: capture}
}

func (context *executionContext) stopRequested() bool {
	if context == nil {
		return false
	}
	if context.stopSeen.Load() {
		return true
	}
	select {
	case <-context.stop:
		context.stopSeen.Store(true)
		return true
	default:
		return false
	}
}

func (context *executionContext) appendOutput(outputType int, data []byte) {
	if context == nil {
		return
	}
	copyOfData := append([]byte(nil), data...)
	output := Output{Type: outputType, Data: copyOfData}
	context.mu.Lock()
	if context.capture {
		context.outputs = append(context.outputs, output)
	}
	emit := context.emit
	context.mu.Unlock()
	if emit != nil {
		// A callback into Go must not panic across the native BOF stack.
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					context.mu.Lock()
					context.emit = nil
					context.mu.Unlock()
					context.addError(fmt.Errorf("BOF output callback panic: %v", recovered))
				}
			}()
			emit(output)
		}()
	}
}

func (context *executionContext) addError(err error) {
	if context == nil || err == nil {
		return
	}
	context.mu.Lock()
	context.errors = append(context.errors, err)
	context.mu.Unlock()
}

func (context *executionContext) allocate(size int) (uintptr, []byte, error) {
	if context == nil {
		return 0, nil, errors.New("no BOF execution is active")
	}
	if size <= 0 || size > maxFormatAllocation {
		return 0, nil, fmt.Errorf("format allocation size %d is outside 1..%d", size, maxFormatAllocation)
	}
	data := make([]byte, size)
	address := byteSliceAddress(data)
	if address == 0 {
		return 0, nil, errors.New("format allocation returned a nil address")
	}
	context.mu.Lock()
	context.allocations[address] = data
	context.mu.Unlock()
	return address, data, nil
}

func (context *executionContext) allocation(address uintptr) ([]byte, bool) {
	if context == nil || address == 0 {
		return nil, false
	}
	context.mu.Lock()
	data, ok := context.allocations[address]
	context.mu.Unlock()
	return data, ok
}

func (context *executionContext) release(address uintptr) {
	if context == nil || address == 0 {
		return
	}
	context.mu.Lock()
	delete(context.allocations, address)
	context.mu.Unlock()
}

func (context *executionContext) result() ([]Output, error) {
	if context == nil {
		return nil, errors.New("nil BOF execution context")
	}
	context.mu.Lock()
	defer context.mu.Unlock()

	outputs := make([]Output, len(context.outputs))
	for index := range context.outputs {
		outputs[index] = Output{
			Type: context.outputs[index].Type,
			Data: append([]byte(nil), context.outputs[index].Data...),
		}
	}
	return outputs, errors.Join(context.errors...)
}
