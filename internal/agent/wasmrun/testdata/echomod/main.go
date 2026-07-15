//go:build wasip1

// Command echomod is a tiny WASM module used to test the runner. It exercises
// the same "armada" host ABI that C modules target: it logs a line and runs a
// shell command through armada.exec. Built at test time with GOOS=wasip1.
package main

import "unsafe"

//go:wasmimport armada exec
func armadaExec(ptr unsafe.Pointer, n int32) int32

//go:wasmimport armada log
func armadaLog(ptr unsafe.Pointer, n int32)

func emit(s string) {
	b := []byte(s)
	armadaLog(unsafe.Pointer(&b[0]), int32(len(b)))
}

func run(s string) int32 {
	b := []byte(s)
	return armadaExec(unsafe.Pointer(&b[0]), int32(len(b)))
}

func main() {
	emit("hello-from-module\n")
	if run("echo exec-output") != 0 {
		emit("exec-nonzero\n")
	}
}
