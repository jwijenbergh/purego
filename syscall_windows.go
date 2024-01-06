// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2022 The Ebitengine Authors

package purego

import (
	"reflect"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

// maxCb is the maximum number of callbacks
const maxCB = 1024

var syscall15XABI0 uintptr

func syscall_syscall15X(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15 uintptr) (r1, r2, err uintptr) {
	r1, r2, errno := syscall.Syscall15(fn, 15, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15)
	return r1, r2, uintptr(errno)
}

// NewCallback converts a Go function to a function pointer conforming to the stdcall calling convention.
// This is useful when interoperating with Windows code requiring callbacks. The argument is expected to be a
// function with one uintptr-sized result. The function must not have arguments with size larger than the
// size of uintptr. Only a limited number of callbacks may be created in a single Go process, and any memory
// allocated for these callbacks is never released. Between NewCallback and NewCallbackCDecl, at least 1024
// callbacks can always be created. Although this function is similiar to the darwin version it may act
// differently.
func NewCallback(fn interface{}) uintptr {
	val := reflect.ValueOf(fn)
	if val.Kind() != reflect.Func {
		panic("purego: the type must be a function but was not")
	}
	if val.IsNil() {
		panic("purego: function must not be nil")
	}
	return syscall.NewCallback(fn)
}

// NewCallbackFnPtr converts a Go function pointer to a function pointer conforming to the stdcall calling convention.
// This is useful when interoperating with C code requiring callbacks. The argument is expected to be a
// function with one uintptr-sized result. The function must not have arguments with size larger than the
// size of uintptr. Only a limited number of callbacks may be created in a single Go process, and any memory
// allocated for these callbacks is never released. Between NewCallback and NewCallbackCDecl, at least 1024
// callbacks can always be created. Although this function is similiar to the darwin version it may act
// differently.
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

	addr := syscall.NewCallback(val.Elem().Interface())

	cbs.lock.Lock()
	cbs.knownFnPtr[val.Pointer()] = addr
	cbs.lock.Unlock()
	return addr
}

var cbs = struct {
	lock       sync.RWMutex
	knownFnPtr map[uintptr]uintptr // maps function pointers to callback addresses
}{
	knownFnPtr: make(map[uintptr]uintptr, maxCB),
}

//go:linkname openLibrary openLibrary
func openLibrary(name string) (uintptr, error) {
	handle, err := windows.LoadLibrary(name)
	return uintptr(handle), err
}

func loadSymbol(handle uintptr, name string) (uintptr, error) {
	return windows.GetProcAddress(windows.Handle(handle), name)
}

func getCallbackByFnPtr(val reflect.Value) (uintptr, bool) {
	cbs.lock.RLock()
	defer cbs.lock.RUnlock()
	addr, ok := cbs.knownFnPtr[val.Pointer()]
	return addr, ok
}
