// Patches upstream Go's lib/wasm/wasm_exec_node.js so it works as the
// GOOS=js GOARCH=wasm test runner for this repo:
//
// 1. Passing the full process env to Go makes it fail with `total length of
//    command line and environment variables exceeds limit`, so we drop that
//    assignment.
// 2. internal/jsutil/jsutil.go reads `js.Global().Get("context").Get("binding")`
//    at init, which requires the patched wasm_exec.js's `run(instance, context)`
//    to be called with a context object (see transform-wasm-exec.ts).
// 3. //go:wasmimport workers ready (see internal/jsworkers or the `workers`
//    package's Ready()) needs a `workers.ready` entry in the importObject
//    passed to WebAssembly.instantiate, or instantiation fails with a
//    LinkError as soon as anything reachable from the test binary references
//    it. We record how many times it was called on the context object so
//    tests can assert on it (see internal/jstest.ReadyCount).
// 4. internal/jsutil.TryCatch and cloudflare/sockets.Connect call
//    globalThis.tryCatch, which only exists in the Workers runtime
//    (cmd/workers-assets-gen/assets/common/worker.mjs). We polyfill the same
//    definition here so those code paths work under Node too.

const ENV_LINE =
	'go.env = Object.assign({ TMPDIR: require("os").tmpdir() }, process.env);';

const ENV_REPLACEMENT = `// to prevent \`total length of command line and environment variables exceeds limit\` error, ignore env.
// go.env = Object.assign({ TMPDIR: require("os").tmpdir() }, process.env);`;

const REQUIRE_WASM_EXEC_LINE = 'require("./wasm_exec");';

const REQUIRE_WASM_EXEC_REPLACEMENT = `require("./wasm_exec");

// same definition as cmd/workers-assets-gen/assets/common/worker.mjs, needed
// by internal/jsutil.TryCatch and cloudflare/sockets.Connect.
globalThis.tryCatch = (fn) => {
	try {
		return {
			result: fn(),
		};
	} catch (e) {
		return {
			error: e,
		};
	}
};`;

const INSTANTIATE_LINE =
	"WebAssembly.instantiate(fs.readFileSync(process.argv[2]), go.importObject).then((result) => {";

const INSTANTIATE_REPLACEMENT = `// the patched wasm_exec.js reads context.binding (see internal/jsutil), and
// go.run(result.instance, context) below passes this object as context.
const context = { binding: {} };
// workers.ready is required by //go:wasmimport workers ready; record how
// many times it's called so tests can assert on it.
const importObject = {
	...go.importObject,
	workers: {
		ready: () => {
			context.readyCount = (context.readyCount ?? 0) + 1;
		},
	},
};
WebAssembly.instantiate(fs.readFileSync(process.argv[2]), importObject).then((result) => {`;

const RUN_LINE = "\treturn go.run(result.instance);";

const RUN_REPLACEMENT = "\treturn go.run(result.instance, context);";

function replaceExactlyOnce(
	source: string,
	target: string,
	replacement: string,
	label: string,
): string {
	const firstIndex = source.indexOf(target);
	if (firstIndex === -1) {
		throw new Error(`transform-node: could not find ${label}`);
	}
	const lastIndex = source.lastIndexOf(target);
	if (lastIndex !== firstIndex) {
		throw new Error(`transform-node: found more than one occurrence of ${label}`);
	}
	return (
		source.slice(0, firstIndex) +
		replacement +
		source.slice(firstIndex + target.length)
	);
}

export function transformNode(rawSource: string): string {
	let result = replaceExactlyOnce(
		rawSource,
		ENV_LINE,
		ENV_REPLACEMENT,
		"the `go.env = Object.assign(...)` line",
	);
	result = replaceExactlyOnce(
		result,
		REQUIRE_WASM_EXEC_LINE,
		REQUIRE_WASM_EXEC_REPLACEMENT,
		'the `require("./wasm_exec");` line',
	);
	result = replaceExactlyOnce(
		result,
		INSTANTIATE_LINE,
		INSTANTIATE_REPLACEMENT,
		"the `WebAssembly.instantiate(...)` line",
	);
	result = replaceExactlyOnce(
		result,
		RUN_LINE,
		RUN_REPLACEMENT,
		"the `return go.run(result.instance);` line",
	);
	return result;
}
