// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

"use strict";

if (process.argv.length < 3) {
	console.error("usage: go_js_wasm_exec [wasm binary] [arguments]");
	process.exit(1);
}

globalThis.require = require;
globalThis.fs = require("fs");
globalThis.path = require("path");
globalThis.TextEncoder = require("util").TextEncoder;
globalThis.TextDecoder = require("util").TextDecoder;

globalThis.performance ??= require("performance");

globalThis.crypto ??= require("crypto");

require("./wasm_exec");

const go = new Go();
go.argv = process.argv.slice(2);
// to prevent `total length of command line and environment variables exceeds limit` error, ignore env.
// go.env = Object.assign({ TMPDIR: require("os").tmpdir() }, process.env);
go.exit = process.exit;
// workers.ready is normally supplied by worker.mjs; stub it here so
// packages that call workers.Ready() (e.g. cloudflare/sockets) can be
// instantiated and tested under this runner too.
const importObject = { ...go.importObject, workers: { ready: () => {} } };
WebAssembly.instantiate(fs.readFileSync(process.argv[2]), importObject).then((result) => {
	process.on("exit", (code) => { // Node.js exits if no event handler is pending
		if (code === 0 && !go.exited) {
			// deadlock, make Go print error and stack traces
			go._pendingEvent = { id: 0 };
			go._resume();
		}
	});
	// the patched wasm_exec.js reads context.binding (see internal/jsutil).
	return go.run(result.instance, { binding: {} });
}).catch((err) => {
	console.error(err);
	process.exit(1);
});
