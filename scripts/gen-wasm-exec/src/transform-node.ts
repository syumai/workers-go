// Patches upstream Go's lib/wasm/wasm_exec_node.js so it works as the
// GOOS=js GOARCH=wasm test runner for this repo:
//
// 1. Passing the full process env to Go makes it fail with `total length of
//    command line and environment variables exceeds limit`, so we drop that
//    assignment.
// 2. internal/jsutil/jsutil.go reads `js.Global().Get("context").Get("binding")`
//    at init, which requires the patched wasm_exec.js's `run(instance, context)`
//    to be called with a context object (see transform-wasm-exec.ts).
// 3. Packages that call workers.Ready() (cloudflare/sockets' Listen/Serve,
//    among others) declare a `//go:wasmimport workers ready` function. The
//    real worker.mjs runtime satisfies that import itself (see
//    cmd/workers-assets-gen/assets/common/worker.mjs's `run()`), but this
//    test runner instantiates the wasm binary directly, so it must supply
//    the same "workers" import module itself or WebAssembly.instantiate
//    fails outright for any test binary that reaches such a call.

const ENV_LINE =
	'go.env = Object.assign({ TMPDIR: require("os").tmpdir() }, process.env);';

const ENV_REPLACEMENT = `// to prevent \`total length of command line and environment variables exceeds limit\` error, ignore env.
// go.env = Object.assign({ TMPDIR: require("os").tmpdir() }, process.env);`;

const IMPORT_OBJECT_LINE =
	"WebAssembly.instantiate(fs.readFileSync(process.argv[2]), go.importObject).then((result) => {";

const IMPORT_OBJECT_REPLACEMENT = `// workers.ready is normally supplied by worker.mjs; stub it here so
// packages that call workers.Ready() (e.g. cloudflare/sockets) can be
// instantiated and tested under this runner too.
const importObject = { ...go.importObject, workers: { ready: () => {} } };
WebAssembly.instantiate(fs.readFileSync(process.argv[2]), importObject).then((result) => {`;

const RUN_LINE = "\treturn go.run(result.instance);";

const RUN_REPLACEMENT = `\t// the patched wasm_exec.js reads context.binding (see internal/jsutil).
\treturn go.run(result.instance, { binding: {} });`;

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
		IMPORT_OBJECT_LINE,
		IMPORT_OBJECT_REPLACEMENT,
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
