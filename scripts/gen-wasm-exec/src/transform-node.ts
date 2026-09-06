// Patches upstream Go's lib/wasm/wasm_exec_node.js so it works as the
// GOOS=js GOARCH=wasm test runner for this repo:
//
// 1. Passing the full process env to Go makes it fail with `total length of
//    command line and environment variables exceeds limit`, so we drop that
//    assignment.
// 2. internal/jsutil/jsutil.go reads `js.Global().Get("context").Get("binding")`
//    at init, which requires the patched wasm_exec.js's `run(instance, context)`
//    to be called with a context object (see transform-wasm-exec.ts).

const ENV_LINE =
	'go.env = Object.assign({ TMPDIR: require("os").tmpdir() }, process.env);';

const ENV_REPLACEMENT = `// to prevent \`total length of command line and environment variables exceeds limit\` error, ignore env.
// go.env = Object.assign({ TMPDIR: require("os").tmpdir() }, process.env);`;

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
		RUN_LINE,
		RUN_REPLACEMENT,
		"the `return go.run(result.instance);` line",
	);
	return result;
}
