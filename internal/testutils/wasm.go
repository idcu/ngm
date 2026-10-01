package testutils

// 手写 WASI 命令模块。
//
// 为什么手写字节而不是用 tinygo / emscripten 编译：CI 里没有那些工具链，
// 而"测试不依赖额外工具"是本项目的硬纪律（见 development/README.md 的测试策略）。
// 一个四五十字节的模块足以证明 adapter 的接线——stdout 捕获、退出码、内存上限、
// 以及"模块没有写权限"——而这些正是要断言的东西。
//
// 放在 testutils 而不是某个包的 _test.go 里：adapter 的单元测试与 CLI 的端到端
// 验收都要用它，而两份拷贝迟早会漂移。本包只被测试导入，因此不进二进制。

// leb 是 unsigned LEB128（wasm 的节长度与索引都用它编码）。
func leb(n int) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

// wasmSection 组装一个节：id + LEB 长度 + 载荷。
func wasmSection(id byte, payload []byte) []byte {
	out := []byte{id}
	out = append(out, leb(len(payload))...)
	return append(out, payload...)
}

// wasmName 编码一个名字（长度 + 字节）。
func wasmName(s string) []byte {
	return append(leb(len(s)), []byte(s)...)
}

const wasiModuleName = "wasi_snapshot_preview1"

// wasmHeader 是每个模块开头固定的 8 字节（magic + version）。
func wasmHeader() []byte { return []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00} }

// WASIModuleStdout 返回一个把 text 写到 stdout 后正常退出的 WASI 命令模块。
//
// 等价于 `(module (import "wasi_snapshot_preview1" "fd_write" ...) ... _start)`：
// 内存里预置好 iovec 与文本，`_start` 调一次 fd_write 就返回。
func WASIModuleStdout(text string) []byte {
	if len(text) > 100 {
		panic("testutils: keep the text short enough for a one-byte length")
	}
	// 内存布局：iovec(0..7)={ptr=12,len}；nwritten(8..11)；文本(12..)
	data := []byte{12, 0, 0, 0, byte(len(text)), 0, 0, 0, 0, 0, 0, 0}
	data = append(data, []byte(text)...)

	// _start: fd_write(1, 0, 1, 8); drop; end
	body := []byte{
		0x00,       // 局部变量数 = 0
		0x41, 0x01, // i32.const 1  (fd = stdout)
		0x41, 0x00, // i32.const 0  (iovs)
		0x41, 0x01, // i32.const 1  (iovs_len)
		0x41, 0x08, // i32.const 8  (nwritten)
		0x10, 0x00, // call 0 (fd_write)
		0x1a, // drop
		0x0b, // end
	}

	types := []byte{0x02,
		0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, // (i32,i32,i32,i32)->i32
		0x60, 0x00, 0x00, // ()->()
	}
	imports := append([]byte{0x01}, wasmName(wasiModuleName)...)
	imports = append(imports, wasmName("fd_write")...)
	imports = append(imports, 0x00, 0x00) // kind=func, type=0

	exports := []byte{0x02}
	exports = append(exports, wasmName("_start")...)
	exports = append(exports, 0x00, 0x01) // func 1（导入占索引 0）
	exports = append(exports, wasmName("memory")...)
	exports = append(exports, 0x02, 0x00) // memory 0

	code := []byte{0x01}
	code = append(code, leb(len(body))...)
	code = append(code, body...)

	dataSeg := append([]byte{0x01, 0x00, 0x41, 0x00, 0x0b}, leb(len(data))...)
	dataSeg = append(dataSeg, data...)

	out := wasmHeader()
	out = append(out, wasmSection(1, types)...)
	out = append(out, wasmSection(2, imports)...)
	out = append(out, wasmSection(3, []byte{0x01, 0x01})...)       // 1 个函数，类型 1
	out = append(out, wasmSection(5, []byte{0x01, 0x00, 0x01})...) // 1 页内存
	out = append(out, wasmSection(7, exports)...)
	out = append(out, wasmSection(10, code)...)
	return append(out, wasmSection(11, dataSeg)...)
}

// WASIModuleExit 返回一个调用 proc_exit(code) 的 WASI 命令模块。
//
// 用来验证"模块自己的退出码会变成引擎失败码"——那是 CI 判断引擎为什么失败的依据。
func WASIModuleExit(code int) []byte {
	body := []byte{0x00, 0x41, byte(code), 0x10, 0x00, 0x0b} // i32.const code; call 0; end

	types := []byte{0x02,
		0x60, 0x01, 0x7f, 0x00, // proc_exit(i32)->()
		0x60, 0x00, 0x00, // _start()->()
	}
	imports := append([]byte{0x01}, wasmName(wasiModuleName)...)
	imports = append(imports, wasmName("proc_exit")...)
	imports = append(imports, 0x00, 0x00)

	exports := []byte{0x01}
	exports = append(exports, wasmName("_start")...)
	exports = append(exports, 0x00, 0x01)

	codeSec := []byte{0x01}
	codeSec = append(codeSec, leb(len(body))...)
	codeSec = append(codeSec, body...)

	out := wasmHeader()
	out = append(out, wasmSection(1, types)...)
	out = append(out, wasmSection(2, imports)...)
	out = append(out, wasmSection(3, []byte{0x01, 0x01})...)
	out = append(out, wasmSection(7, exports)...)
	return append(out, wasmSection(10, codeSec)...)
}

// WASIModuleMemoryPages 返回一个什么都不做、正常退出，但声明 pages 页内存的模块。
//
// 用来验证内存上限：声明超过上限时，模块应当在**实例化阶段**就被拒绝。
func WASIModuleMemoryPages(pages int) []byte {
	body := []byte{0x00, 0x0b}

	types := []byte{0x01, 0x60, 0x00, 0x00}
	exports := []byte{0x01}
	exports = append(exports, wasmName("_start")...)
	exports = append(exports, 0x00, 0x00) // 无导入，_start 是函数 0

	code := []byte{0x01}
	code = append(code, leb(len(body))...)
	code = append(code, body...)

	out := wasmHeader()
	out = append(out, wasmSection(1, types)...)
	out = append(out, wasmSection(3, []byte{0x01, 0x00})...)
	mem := append([]byte{0x01, 0x00}, leb(pages)...) // flags=0（只有下限）+ 下限
	out = append(out, wasmSection(5, mem)...)
	out = append(out, wasmSection(7, exports)...)
	return append(out, wasmSection(10, code)...)
}
