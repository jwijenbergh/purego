// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2022 The Ebitengine Authors

package strings

import (
	"unsafe"
)

// hasSuffix tests whether the string s ends with suffix.
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// ByteSlice creates a pointer to a byte slice of C strings
func ByteSlice(name []string) **byte {
	if name == nil {
		return nil
	}
	res := make([]*byte, len(name)+1)
	for i, v := range name {
		res[i] = CString(v)
	}

	// the last element is NULL terminated for GTK
	res[len(name)] = nil
	return &res[0]
}

// CString converts a go string to *byte that can be passed to C code.
func CString(name string) *byte {
	if hasSuffix(name, "\x00") {
		return &(*(*[]byte)(unsafe.Pointer(&name)))[0]
	}
	b := make([]byte, len(name)+1)
	copy(b, name)
	return &b[0]
}

// GoStringSlice gets a string slice from a char** array
func GoStringSlice(c uintptr) []string {
	var ret []string
	for i := 0; ; i++ {
		ptrAddr := c + uintptr(i)*unsafe.Sizeof(uintptr(0))
		addr := *(*unsafe.Pointer)(unsafe.Pointer(&ptrAddr))
		// We take the address and then dereference it to trick go vet from creating a possible misuse of unsafe.Pointer
		ptr := *(*uintptr)(addr)
		if ptr == 0 {
			break
		}
		ret = append(ret, GoString(ptr))
	}

	return ret
}

// GoString copies a null-terminated char* to a Go string.
func GoString(c uintptr) string {
	// We take the address and then dereference it to trick go vet from creating a possible misuse of unsafe.Pointer
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&c))
	if ptr == nil {
		return ""
	}
	var length int
	for {
		if *(*byte)(unsafe.Add(ptr, uintptr(length))) == '\x00' {
			break
		}
		length++
	}
	return string(unsafe.Slice((*byte)(ptr), length))
}
