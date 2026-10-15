package bofloader

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

const maxFormatAllocation = 16 << 20

const (
	maxManagedWorkers     = 32
	maxManagedWorkerBytes = 16 << 20
)

var executionContexts sync.Map // map[uint64]*executionContext, keyed by native OS thread ID

func registerExecutionContext(context *executionContext) (func(), error) {
	threadID, err := currentExecutionThreadID()
	if err != nil {
		return nil, fmt.Errorf("identify BOF execution thread: %w", err)
	}
	if _, loaded := executionContexts.LoadOrStore(threadID, context); loaded {
		return nil, fmt.Errorf("BOF execution context already active on thread %d", threadID)
	}
	context.mu.Lock()
	if !context.entryThreadSet {
		context.entryThreadID = threadID
		context.entryThreadSet = true
	}
	context.mu.Unlock()
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

// InvokeHostCallback runs an opt-in host service on a registered BOF thread.
// It converts service errors and panics into Execute errors so neither crosses
// the native callback boundary.
func InvokeHostCallback(name string, invoke func() (uintptr, error)) (result uintptr) {
	context := activeExecutionContext()
	if context == nil {
		return 0
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			context.addError(fmt.Errorf("%s host callback panic: %v", name, recovered))
			result = 0
		}
	}()
	result, err := invoke()
	if err != nil {
		context.addError(fmt.Errorf("%s host callback: %w", name, err))
		return 0
	}
	return result
}

// InvokeEntryHostCallback refuses token services on managed workers. Token
// cleanup is scoped to the original execution thread, so allowing a worker to
// impersonate would leave that worker's thread state outside its cleanup.
func InvokeEntryHostCallback(name string, invoke func() (uintptr, error)) uintptr {
	context := activeExecutionContext()
	if context == nil {
		return 0
	}
	threadID, err := currentExecutionThreadID()
	if err != nil {
		context.addError(fmt.Errorf("%s host callback: identify execution thread: %w", name, err))
		return 0
	}
	context.mu.Lock()
	entry := context.entryThreadSet && threadID == context.entryThreadID
	context.mu.Unlock()
	if !entry {
		context.addError(fmt.Errorf("%s host callback requires the BOF entry thread", name))
		return 0
	}
	return InvokeHostCallback(name, invoke)
}

type executionContext struct {
	mu             sync.Mutex
	outputs        []Output
	errors         []error
	allocations    map[uintptr][]byte
	cleanups       map[any]hostCleanup
	cleanupKeys    []any
	emit           func(Output)
	capture        bool
	stop           <-chan struct{}
	stopSeen       atomic.Bool
	entryThreadID  uint64
	entryThreadSet bool
	workersEnabled bool
	workerRanges   []executableRange
	workers        int
	workerBytes    int
	workerCond     *sync.Cond
}

type hostCleanup struct {
	name string
	call func() error
}

// RegisterHostCleanup schedules a service cleanup on the BOF entry thread.
// Re-registering the same key replaces its pending cleanup.
func RegisterHostCleanup(key any, name string, call func() error) error {
	context := activeExecutionContext()
	if context == nil {
		return errors.New("no BOF execution is active")
	}
	threadID, err := currentExecutionThreadID()
	if err != nil {
		return fmt.Errorf("identify BOF execution thread: %w", err)
	}
	context.mu.Lock()
	defer context.mu.Unlock()
	if !context.entryThreadSet || threadID != context.entryThreadID {
		return errors.New("host cleanup may be registered only on the BOF entry thread")
	}
	if context.cleanups == nil {
		context.cleanups = make(map[any]hostCleanup)
	}
	if _, exists := context.cleanups[key]; !exists {
		context.cleanupKeys = append(context.cleanupKeys, key)
	}
	context.cleanups[key] = hostCleanup{name: name, call: call}
	return nil
}

// ClearHostCleanup cancels a pending cleanup after an explicit successful
// service cleanup. It has no effect outside a BOF execution.
func ClearHostCleanup(key any) {
	context := activeExecutionContext()
	if context == nil {
		return
	}
	threadID, err := currentExecutionThreadID()
	if err != nil {
		return
	}
	context.mu.Lock()
	if !context.entryThreadSet || threadID != context.entryThreadID {
		context.mu.Unlock()
		return
	}
	delete(context.cleanups, key)
	for index, registered := range context.cleanupKeys {
		if registered == key {
			context.cleanupKeys = append(context.cleanupKeys[:index], context.cleanupKeys[index+1:]...)
			break
		}
	}
	context.mu.Unlock()
}

func (context *executionContext) runHostCleanups() {
	context.mu.Lock()
	keys := context.cleanupKeys
	cleanups := context.cleanups
	context.cleanupKeys = nil
	context.cleanups = nil
	context.mu.Unlock()
	for index := len(keys) - 1; index >= 0; index-- {
		cleanup, ok := cleanups[keys[index]]
		if !ok {
			continue
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					context.addError(fmt.Errorf("%s host cleanup panic: %v", cleanup.name, recovered))
				}
			}()
			if err := cleanup.call(); err != nil {
				context.addError(fmt.Errorf("%s host cleanup: %w", cleanup.name, err))
			}
		}()
	}
}

func newExecutionContext(emit func(Output), capture bool) *executionContext {
	context := &executionContext{allocations: make(map[uintptr][]byte), emit: emit, capture: capture}
	context.workerCond = sync.NewCond(&context.mu)
	return context
}

type executableRange struct {
	address uintptr
	size    uint64
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
	defer context.mu.Unlock()
	if context.capture {
		context.outputs = append(context.outputs, output)
	}
	if context.emit != nil {
		// A callback into Go must not panic across the native BOF stack.
		// Keep delivery under this lock so all threads observe the same order
		// for captured and streamed records.
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					context.emit = nil
					context.errors = append(context.errors, fmt.Errorf("BOF output callback panic: %v", recovered))
				}
			}()
			context.emit(output)
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

// startWorker reserves a managed thread before launching it. The reservation
// remains live until the native function has returned and its callback context
// has been removed, so Execute cannot release the image while it is in use.
func (context *executionContext) startWorker(entry, dataAddress, lengthValue uintptr) uintptr {
	if context == nil {
		return 0
	}
	length := int(int32(lengthValue))
	context.mu.Lock()
	var reason string
	switch {
	case !context.workersEnabled:
		reason = "managed workers are disabled for this execution"
	case length < 0 || length > maxCallbackData:
		reason = fmt.Sprintf("worker argument length %d is outside 0..%d", length, maxCallbackData)
	case length != 0 && dataAddress == 0:
		reason = "nonempty worker arguments have a nil address"
	case context.workers >= maxManagedWorkers:
		reason = fmt.Sprintf("at most %d managed workers may be active", maxManagedWorkers)
	case length > maxManagedWorkerBytes-context.workerBytes:
		reason = fmt.Sprintf("managed worker arguments exceed %d bytes", maxManagedWorkerBytes)
	default:
		valid := false
		for _, region := range context.workerRanges {
			if entry >= region.address && uint64(entry-region.address) < region.size {
				valid = true
				break
			}
		}
		if !valid {
			reason = "worker address is not in a mapped executable BOF section"
		}
	}
	if reason != "" {
		context.errors = append(context.errors, fmt.Errorf("ReflektorStartWorker: %s", reason))
		context.mu.Unlock()
		return 0
	}
	context.workers++
	context.workerBytes += length
	context.mu.Unlock()

	launched := false
	defer func() {
		if !launched {
			context.finishWorker(length)
		}
	}()
	data := append([]byte(nil), pointerBytes(dataAddress, length)...)
	go context.runWorker(entry, data, length)
	launched = true
	return 1
}

func (context *executionContext) runWorker(entry uintptr, data []byte, length int) {
	defer context.finishWorker(length)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		if recovered := recover(); recovered != nil {
			context.addError(fmt.Errorf("managed BOF worker panic: %v", recovered))
		}
	}()
	unregister, err := registerExecutionContext(context)
	if err != nil {
		context.addError(fmt.Errorf("register managed BOF worker: %w", err))
		return
	}
	defer unregister()
	invokeEntry(entry, byteSliceAddress(data), int32(len(data)))
	runtime.KeepAlive(data)
}

func (context *executionContext) finishWorker(length int) {
	context.mu.Lock()
	context.workers--
	context.workerBytes -= length
	context.workerCond.Broadcast()
	context.mu.Unlock()
}

func (context *executionContext) waitWorkers() {
	context.mu.Lock()
	for context.workers != 0 {
		context.workerCond.Wait()
	}
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
