//go:build wasip1

// Package sdk is imported by WASM module authors.
// It wraps the host functions exposed by LalaDashboard core.
package sdk

import "unsafe"

//go:wasmimport env http_get
func hostHTTPGet(urlPtr uint32, urlLen uint32, resultPtr uint32) uint32

//go:wasmimport env http_post
func hostHTTPPost(urlPtr uint32, urlLen uint32, bodyPtr uint32, bodyLen uint32, resultPtr uint32) uint32

//go:wasmimport env log_message
func hostLog(msgPtr uint32, msgLen uint32)

// resultBuf holds the last host-call result.
var resultBuf [1 << 20]byte // 1 MB max response

// HTTPGet performs a GET request via the host and returns the response body.
func HTTPGet(url string) string {
	urlBytes := []byte(url)
	n := hostHTTPGet(
		uint32(uintptr(unsafe.Pointer(&urlBytes[0]))),
		uint32(len(urlBytes)),
		uint32(uintptr(unsafe.Pointer(&resultBuf[0]))),
	)
	return string(resultBuf[:n])
}

// HTTPPost performs a POST request via the host and returns the response body.
func HTTPPost(url, body string) string {
	urlBytes := []byte(url)
	bodyBytes := []byte(body)
	n := hostHTTPPost(
		uint32(uintptr(unsafe.Pointer(&urlBytes[0]))),
		uint32(len(urlBytes)),
		uint32(uintptr(unsafe.Pointer(&bodyBytes[0]))),
		uint32(len(bodyBytes)),
		uint32(uintptr(unsafe.Pointer(&resultBuf[0]))),
	)
	return string(resultBuf[:n])
}

// Log sends a message to the LalaDashboard server log.
func Log(msg string) {
	b := []byte(msg)
	hostLog(uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

// ReadString reads a string from WASM linear memory given a pointer and length.
// Used by exported functions to receive string parameters from the host.
func ReadString(ptr, length int32) string {
	b := make([]byte, length)
	for i := int32(0); i < length; i++ {
		b[i] = *(*byte)(unsafe.Pointer(uintptr(ptr) + uintptr(i)))
	}
	return string(b)
}

var outBuf [1 << 20]byte
var lastLen int32

// SetOutput writes s into the output buffer so the host can read it via
// GetOutputPtr / GetOutputLen after an exported function returns.
func SetOutput(s string) {
	n := copy(outBuf[:], s)
	lastLen = int32(n)
}

//export get_output_ptr
func GetOutputPtr() int32 {
	return int32(uintptr(unsafe.Pointer(&outBuf[0])))
}

//export get_output_len
func GetOutputLen() int32 {
	return lastLen
}
