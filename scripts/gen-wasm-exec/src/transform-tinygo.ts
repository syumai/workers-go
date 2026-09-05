import { transformWasmExec } from "./transform-wasm-exec.ts";

// TinyGo's targets/wasm_exec.js has shipped two distinct layouts:
//
// - Legacy (< 0.42.0): mirrors an older upstream Go wasm_exec.js. Uses
//   `global` instead of `globalThis` (via the "Map multiple JavaScript
//   environments" shim near the top of the file) and wraps a handful of
//   Node.js-only blocks (require() polyfills, crypto/TextEncoder polyfills
//   via require, and a `require.main === module` runner) that must be
//   commented out before the shared transform runs, since they don't exist
//   in the browser/Workers runtime. Inside run(), there is 1 standalone
//   (non-member-expression) occurrence of `global`: the `_values` array
//   entry.
//
// - Current (>= 0.42.0): rewritten to mirror upstream Go's current
//   structure. Uses `globalThis` everywhere and has no Node.js-only blocks
//   at all (just `if (!globalThis.crypto) { throw ... }` style checks, same
//   as Go), so there is nothing to comment out. Inside run(), there is
//   still only 1 standalone occurrence of `globalThis` (the `_values` array
//   entry) — unlike Go, which also assigns to `globalThis` directly and so
//   has 2.
//
// We detect which layout we were given by checking whether the source
// declares `global.Go = class` (legacy) or `globalThis.Go = class`
// (current).

// Top-level Node.js-only blocks shipped by legacy (< 0.42.0) TinyGo. Each
// entry pins the exact opening substring; `closes` says how many "\n\t}"
// lines to skip past — TextEncoder is paired with TextDecoder under a
// single /* */, so it needs closes=2.
const NODEJS_BLOCKS: Array<{ opening: string; closes: number }> = [
	{
		opening: '\tif (!global.require && typeof require !== "undefined") {',
		closes: 1,
	},
	{ opening: "\tif (!global.fs && global.require) {", closes: 1 },
	{ opening: "\tif (!global.crypto) {", closes: 1 },
	{ opening: "\tif (!global.TextEncoder) {", closes: 2 },
	{
		opening: "\tif (\n\t\tglobal.require &&\n\t\tglobal.require.main === module &&",
		closes: 1,
	},
];

interface Range {
	start: number;
	end: number;
}

function findBlockRanges(source: string): Range[] {
	const ranges: Range[] = [];
	for (const block of NODEJS_BLOCKS) {
		const start = source.indexOf(block.opening);
		if (start === -1) {
			throw new Error(
				`transform-tinygo: could not find block opening: ${block.opening.slice(0, 50)}…`,
			);
		}
		let pos = start;
		for (let i = 0; i < block.closes; i++) {
			const closeNL = source.indexOf("\n\t}", pos);
			if (closeNL === -1) {
				throw new Error(
					`transform-tinygo: could not find matching close for: ${block.opening.slice(0, 50)}…`,
				);
			}
			pos = closeNL + 3;
		}
		ranges.push({ start, end: pos });
	}
	return ranges;
}

function commentOutNodejsBlocks(rawSource: string): string {
	const ranges = findBlockRanges(rawSource).sort((a, b) => b.start - a.start);
	let result = rawSource;
	for (const m of ranges) {
		const block = result.slice(m.start, m.end);
		result = `${result.slice(0, m.start)}\t/*\n${block}\n\t*/${result.slice(m.end)}`;
	}
	return result;
}

export function transformTinygo(rawSource: string): string {
	const isCurrentLayout = rawSource.includes("globalThis.Go = class");
	if (isCurrentLayout) {
		return transformWasmExec(rawSource, {
			flavor: "tinygo",
			globalName: "globalThis",
			expectedStandaloneGlobals: 1,
		});
	}
	return transformWasmExec(commentOutNodejsBlocks(rawSource), {
		flavor: "tinygo",
		globalName: "global",
		expectedStandaloneGlobals: 1,
	});
}
