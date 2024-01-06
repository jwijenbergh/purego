// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2022 The Ebitengine Authors

//go:build darwin || freebsd || (linux && (amd64 || arm64))

package purego

import (
	"errors"
	"reflect"
	"runtime"
	"sync"
	"unsafe"

	"github.com/jwijenbergh/purego/internal/strings"
)

var syscall15XABI0 uintptr

//go:nosplit
func syscall_syscall15X(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15 uintptr) (r1, r2, err uintptr) {
	args := syscall15Args{
		fn, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15,
		a1, a2, a3, a4, a5, a6, a7, a8,
		0,
	}
	runtime_cgocall(syscall15XABI0, unsafe.Pointer(&args))
	return args.a1, args.a2, 0
}

// UnrefCallback unreferences the associated callback (created by NewCallback) by callback pointer.
func UnrefCallback(cb uintptr) error {
	cbs.lock.Lock()
	defer cbs.lock.Unlock()
	idx, ok := cbs.knownIdx[cb]
	if !ok {
		return errors.New(`callback not found`)
	}
	val := cbs.funcs[idx]
	delete(cbs.knownFnPtr, val.Pointer())
	delete(cbs.knownIdx, cb)
	cbs.holes[idx] = struct{}{}
	cbs.funcs[idx] = reflect.Value{}
	return nil
}

// UnrefCallbackFnPtr unreferences the associated callback (created by NewCallbackFnPtr) by function pointer address
func UnrefCallbackFnPtr(cb any) error {
	val := reflect.ValueOf(cb)
	if val.IsNil() {
		panic("purego: function must not be nil")
	}
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Func {
		panic("purego: the type must be a function pointer but was not")
	}

	addr, ok := getCallbackByFnPtr(val)
	if !ok {
		return errors.New(`callback not found`)
	}

	cbs.lock.Lock()
	defer cbs.lock.Unlock()
	idx := cbs.knownIdx[addr]
	delete(cbs.knownFnPtr, val.Pointer())
	delete(cbs.knownIdx, addr)
	cbs.holes[idx] = struct{}{}
	cbs.funcs[idx] = reflect.Value{}
	return nil
}

// NewCallback converts a Go function to a function pointer conforming to the C calling convention.
// This is useful when interoperating with C code requiring callbacks. The argument is expected to be a
// function with zero or one uintptr-sized result. The function must not have arguments with size larger than the size
// of uintptr. Only a limited number of callbacks may be live in a single Go process, and any memory allocated
// for these callbacks is not released until CallbackUnref is called. At most 2000 callbacks can always be live.
// Although this function provides similar functionality to windows.NewCallback it is distinct.
func NewCallback(fn interface{}) uintptr {
	val := reflect.ValueOf(fn)
	if val.Kind() != reflect.Func {
		panic("purego: the type must be a function but was not")
	}
	if val.IsNil() {
		panic("purego: function must not be nil")
	}
	return compileCallback(val)
}

// NewCallbackFnPtr converts a Go function pointer to a function pointer conforming to the C calling convention.
// This is useful when interoperating with C code requiring callbacks. The argument is expected to be a
// function with zero or one uintptr-sized result. The function must not have arguments with size larger than the size
// of uintptr. Only a limited number of callbacks may be live in a single Go process, and any memory allocated
// for these callbacks is not released until CallbackUnrefFnPtr is called. At most 2000 callbacks can always be live.
//
// Calling this function multiple times with the same function pointer will return the originally created callback
// reference, reducing live callback pressure.
func NewCallbackFnPtr(fnptr interface{}) uintptr {
	val := reflect.ValueOf(fnptr)
	if val.IsNil() {
		panic("purego: function must not be nil")
	}
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Func {
		panic("purego: the type must be a function pointer but was not")
	}

	// Re-use callback to function pointer if available
	if addr, ok := getCallbackByFnPtr(val); ok {
		return addr
	}

	addr := compileCallback(val.Elem())

	cbs.lock.Lock()
	cbs.knownFnPtr[val.Pointer()] = addr
	cbs.lock.Unlock()
	return addr
}

// maxCb is the maximum number of callbacks
// only increase this if you have added more to the callbackasm function
const maxCB = 2000

var cbs = struct {
	lock       sync.RWMutex
	holes      map[int]struct{}     // tracks available indexes in the funcs array
	funcs      [maxCB]reflect.Value // the saved callbacks
	knownIdx   map[uintptr]int      // maps callback addresses to index in funcs
	knownFnPtr map[uintptr]uintptr  // maps function pointers to callback addresses
}{
	holes:      make(map[int]struct{}, maxCB),
	knownIdx:   make(map[uintptr]int, maxCB),
	knownFnPtr: make(map[uintptr]uintptr, maxCB),
}

func init() {
	for i := 0; i < maxCB; i++ {
		cbs.holes[i] = struct{}{}
	}
}

func getCallbackByFnPtr(val reflect.Value) (uintptr, bool) {
	cbs.lock.RLock()
	defer cbs.lock.RUnlock()
	addr, ok := cbs.knownFnPtr[val.Pointer()]
	return addr, ok
}

type callbackArgs struct {
	index uintptr
	// args points to the argument block.
	//
	// The structure of the arguments goes
	// float registers followed by the
	// integer registers followed by the stack.
	//
	// This variable is treated as a continuous
	// block of memory containing all of the arguments
	// for this callback.
	args unsafe.Pointer
	// Below are out-args from callbackWrap
	result uintptr
}

func compileCallback(val reflect.Value) uintptr {
	ty := val.Type()
	for i := 0; i < ty.NumIn(); i++ {
		in := ty.In(i)
		switch in.Kind() {
		case reflect.Struct:
			if i == 0 && in.AssignableTo(reflect.TypeOf(CDecl{})) {
				continue
			}
			fallthrough
		case reflect.Interface, reflect.Func, reflect.Slice,
			reflect.Chan, reflect.Complex64, reflect.Complex128,
			reflect.Map, reflect.Invalid:
			panic("purego: unsupported argument type: " + in.Kind().String())
		}
	}
output:
	switch {
	case ty.NumOut() == 1:
		switch ty.Out(0).Kind() {
		case reflect.Pointer, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
			reflect.Bool, reflect.UnsafePointer:
			break output
		}
		panic("purego: unsupported return type: " + ty.String())
	case ty.NumOut() > 1:
		panic("purego: callbacks can only have one return")
	}
	cbs.lock.Lock()
	defer cbs.lock.Unlock()
	if len(cbs.holes) == 0 {
		panic("purego: the maximum number of callbacks has been reached")
	}
	var idx int
	for i := range cbs.holes {
		idx = i
		break
	}
	delete(cbs.holes, idx)
	cbs.funcs[idx] = val
	addr := callbackasmAddr(idx)
	cbs.knownIdx[addr] = idx
	return addr
}

const ptrSize = unsafe.Sizeof((*int)(nil))

const callbackMaxFrame = 64 * ptrSize

// callbackasm is implemented in zcallback_GOOS_GOARCH.s
//
//go:linkname __callbackasm callbackasm
var __callbackasm byte
var callbackasmABI0 = uintptr(unsafe.Pointer(&__callbackasm))

// callbackWrap_call allows the calling of the ABIInternal wrapper
// which is required for runtime.cgocallback without the
// <ABIInternal> tag which is only allowed in the runtime.
// This closure is used inside sys_darwin_GOARCH.s
var callbackWrap_call = callbackWrap

// callbackWrap is called by assembly code which determines which Go function to call.
// This function takes the arguments and passes them to the Go function and returns the result.
func callbackWrap(a *callbackArgs) {
	cbs.lock.RLock()
	fn := cbs.funcs[a.index]
	cbs.lock.RUnlock()
	fnType := fn.Type()
	args := make([]reflect.Value, fnType.NumIn())
	frame := (*[callbackMaxFrame]uintptr)(a.args)
	var floatsN int // floatsN represents the number of float arguments processed
	var intsN int   // intsN represents the number of integer arguments processed
	// stack points to the index into frame of the current stack element.
	// The stack begins after the float and integer registers.
	stack := numOfIntegerRegisters() + numOfFloats
	for i := range args {
		var pos int
		addInt := func() {
			if intsN >= numOfIntegerRegisters() {
				pos = stack
				stack++
			} else {
				// the integers begin after the floats in frame
				pos = intsN + numOfFloats
			}
			intsN++
		}
		switch fnType.In(i).Kind() {
		case reflect.Float32, reflect.Float64:
			if floatsN >= numOfFloats {
				pos = stack
				stack++
			} else {
				pos = floatsN
			}
			floatsN++
		case reflect.String:
			addInt()
			args[i] = reflect.ValueOf(strings.GoString(frame[pos]))
			continue
		case reflect.Struct:
			// This is the CDecl field
			args[i] = reflect.Zero(fnType.In(i))
			continue
		default:
			addInt()
		}
		args[i] = reflect.NewAt(fnType.In(i), unsafe.Pointer(&frame[pos])).Elem()
	}
	ret := fn.Call(args)
	if len(ret) > 0 {
		switch k := ret[0].Kind(); k {
		case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uintptr:
			a.result = uintptr(ret[0].Uint())
		case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
			a.result = uintptr(ret[0].Int())
		case reflect.Bool:
			if ret[0].Bool() {
				a.result = 1
			} else {
				a.result = 0
			}
		case reflect.Pointer:
			a.result = ret[0].Pointer()
		case reflect.UnsafePointer:
			a.result = ret[0].Pointer()
		default:
			panic("purego: unsupported kind: " + k.String())
		}
	}
}

// callbackasmAddr returns address of runtime.callbackasm
// function adjusted by i.
// On x86 and amd64, runtime.callbackasm is a series of CALL instructions,
// and we want callback to arrive at
// correspondent call instruction instead of start of
// runtime.callbackasm.
// On ARM, runtime.callbackasm is a series of mov and branch instructions.
// R12 is loaded with the callback index. Each entry is two instructions,
// hence 8 bytes.
func callbackasmAddr(i int) uintptr {
	var entrySize int
	switch runtime.GOARCH {
	default:
		panic("purego: unsupported architecture")
	case "386", "amd64":
		entrySize = 5
	case "arm", "arm64":
		// On ARM and ARM64, each entry is a MOV instruction
		// followed by a branch instruction
		entrySize = 8
	}
	return callbackasmABI0 + uintptr(i*entrySize)
}
