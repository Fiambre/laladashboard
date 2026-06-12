//go:build wasip1

// Example WASM module for LalaDashboard.
// Compile with: GOOS=wasip1 GOARCH=wasm go build -o widget.wasm .
package main

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

func main() {}

// outBuf holds the output string for host to read.
var outBuf [1 << 20]byte
var outLen int32

func setOutput(s string) {
	n := copy(outBuf[:], s)
	outLen = int32(n)
}

//export get_output_ptr
func getOutputPtr() int32 { return int32(uintptr(unsafe.Pointer(&outBuf[0]))) }

//export get_output_len
func getOutputLen() int32 { return outLen }

//export module_name
func moduleName() int32 {
	setOutput("Mi Widget de Ejemplo")
	return 0
}

//export config_schema
func configSchema() int32 {
	schema := `[
		{"key":"message","label":"Mensaje","type":"text","default":"Hola desde WASM!"},
		{"key":"color","label":"Color","type":"select","default":"blue","options":["blue","green","red"]}
	]`
	setOutput(schema)
	return 0
}

// allocBuf is a module-level buffer for host-written config data.
// Using a package-level var prevents the GC from reclaiming the memory
// before the host finishes writing to the returned pointer.
var allocBuf []byte

//export alloc
func alloc(size uint32) uint32 {
	if uint32(cap(allocBuf)) < size {
		allocBuf = make([]byte, size)
	}
	allocBuf = allocBuf[:size]
	return uint32(uintptr(unsafe.Pointer(&allocBuf[0])))
}

//export render
func render(cfgPtr, cfgLen uint32) int32 {
	// Read config JSON from WASM memory
	cfgBytes := make([]byte, cfgLen)
	for i := uint32(0); i < cfgLen; i++ {
		cfgBytes[i] = *(*byte)(unsafe.Pointer(uintptr(cfgPtr) + uintptr(i)))
	}

	var settings map[string]string
	json.Unmarshal(cfgBytes, &settings)

	msg := settings["message"]
	if msg == "" {
		msg = "Hola desde WASM!"
	}
	color := settings["color"]
	if color == "" {
		color = "blue"
	}

	html := fmt.Sprintf(`
		<div style="text-align:center;padding:1rem;">
			<div style="font-size:2rem;">🚀</div>
			<p style="color:%s;font-weight:600;margin-top:0.5rem;">%s</p>
			<small style="color:#888">Widget WASM externo</small>
		</div>`, color, msg)

	setOutput(html)
	return 0
}
