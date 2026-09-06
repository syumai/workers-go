const RAW_GITHUB = "https://raw.githubusercontent.com/";

export function goLibWasmURL(version: string, file: string): string {
	return new URL(
		`golang/go/refs/tags/go${version}/lib/wasm/${file}`,
		RAW_GITHUB,
	).toString();
}

export function goWasmExecURL(version: string): string {
	return goLibWasmURL(version, "wasm_exec.js");
}

export function goWasmExecNodeURL(version: string): string {
	return goLibWasmURL(version, "wasm_exec_node.js");
}

export function goJsWasmExecURL(version: string): string {
	return goLibWasmURL(version, "go_js_wasm_exec");
}

export function tinygoWasmExecURL(version: string): string {
	return new URL(
		`tinygo-org/tinygo/refs/tags/v${version}/targets/wasm_exec.js`,
		RAW_GITHUB,
	).toString();
}

export async function fetchText(url: string): Promise<string> {
	const res = await fetch(url);
	if (!res.ok) {
		throw new Error(`failed to fetch ${url}: ${res.status} ${res.statusText}`);
	}
	return await res.text();
}
