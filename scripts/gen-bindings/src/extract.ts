// extract.ts converts the @cloudflare/workers-types .d.ts file into a
// deterministic JSON IR (intermediate representation) consumed by the Go
// code generator (cfgen). It walks the TypeScript AST directly and does not
// use the type checker: type references (`ref`) are emitted using the name
// exactly as written in the source, unresolved.
import ts from "typescript";
import { mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

// ---------------------------------------------------------------------------
// CLI args
// ---------------------------------------------------------------------------

interface Args {
	out: string;
}

function parseArgs(argv: string[]): Args {
	const args: Args = { out: path.join("..", "..", "exp", "internal", "gen", "ir") };
	for (let i = 0; i < argv.length; i++) {
		const a = argv[i];
		const next = argv[i + 1];
		switch (a) {
			case "--out":
				if (!next) throw new Error("--out requires a value");
				args.out = next;
				i++;
				break;
			default:
				throw new Error(`unknown argument: ${a}`);
		}
	}
	return args;
}

// ---------------------------------------------------------------------------
// Locate the workers-types entry point
// ---------------------------------------------------------------------------

const DATE_DIR_RE = /^\d{4}-\d{2}-\d{2}$/;

interface EntryPoint {
	version: string;
	entry: string; // relative to the workers-types package root, e.g. "2025-09-01/index.d.ts" or "index.d.ts"
	file: string; // absolute path to the .d.ts file
	date: string; // YYYY-MM-DD used for the SOURCE file
}

function findEntryPoint(): EntryPoint {
	const pkgDir = path.join(
		process.cwd(),
		"node_modules",
		"@cloudflare",
		"workers-types",
	);
	const pkgJSON = JSON.parse(
		readFileSync(path.join(pkgDir, "package.json"), "utf8"),
	) as { version: string };
	const version = pkgJSON.version;

	// Older releases of @cloudflare/workers-types ship one directory per
	// compatibility date (YYYY-MM-DD), excluding "experimental" and "oldest".
	// Pick the lexicographically largest one.
	const dateDirs = readdirSync(pkgDir, { withFileTypes: true })
		.filter((e) => e.isDirectory() && DATE_DIR_RE.test(e.name))
		.map((e) => e.name)
		.sort();

	if (dateDirs.length > 0) {
		const date = dateDirs[dateDirs.length - 1]!;
		return {
			version,
			entry: path.join(date, "index.d.ts"),
			file: path.join(pkgDir, date, "index.d.ts"),
			date,
		};
	}

	// Newer releases ship a single flat index.d.ts at the package root.
	// Derive a date from the version's embedded compat-date segment
	// (e.g. "5.20260906.1" -> "2026-09-06") when possible.
	const m = version.match(/\.(\d{4})(\d{2})(\d{2})\./);
	const date = m ? `${m[1]}-${m[2]}-${m[3]}` : new Date().toISOString().slice(0, 10);
	return {
		version,
		entry: "index.d.ts",
		file: path.join(pkgDir, "index.d.ts"),
		date,
	};
}

// ---------------------------------------------------------------------------
// IR types (mirrors 06-codegen-spec.md section 1.2 exactly)
// ---------------------------------------------------------------------------

// The IR is serialized straight to JSON, so a loosely-typed record is used
// for every node shape rather than a full discriminated-union type — the
// authoritative schema is 06-codegen-spec.md section 1.2, and the Go side
// (cfgen) is responsible for strict decoding.
type IRNode = Record<string, unknown>;
type IRType = IRNode;
type IRParam = IRNode;
type IRMember = IRNode;
type IRTypeParam = IRNode;
type IRDecl = IRNode;

// ---------------------------------------------------------------------------
// Doc comment extraction
// ---------------------------------------------------------------------------

function getDocComment(node: ts.Node, sourceFile: ts.SourceFile): string {
	const fullText = sourceFile.text;
	const ranges = ts.getLeadingCommentRanges(fullText, node.getFullStart());
	if (!ranges || ranges.length === 0) return "";
	// The JSDoc comment, if any, is the block comment immediately preceding
	// the node (i.e. the last comment range).
	const range = ranges[ranges.length - 1]!;
	if (range.kind !== ts.SyntaxKind.MultiLineCommentTrivia) return "";
	const raw = fullText.slice(range.pos, range.end);
	if (!raw.startsWith("/**")) return "";
	const inner = raw.slice(3, raw.length - 2);
	const lines = inner.split("\n").map((line) => {
		let l = line.trim();
		if (l.startsWith("*")) l = l.slice(1);
		return l.trim();
	});
	// Drop leading/trailing blank lines produced by the /** and */ delimiters.
	while (lines.length > 0 && lines[0] === "") lines.shift();
	while (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
	return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Name helpers
// ---------------------------------------------------------------------------

function entityNameText(name: ts.EntityName): string {
	if (ts.isIdentifier(name)) return name.text;
	return `${entityNameText(name.left)}.${name.right.text}`;
}

function moduleNameText(name: ts.Identifier | ts.QualifiedName | ts.StringLiteral): string {
	if (ts.isStringLiteral(name)) return name.text;
	return entityNameText(name);
}

function heritageExprText(expr: ts.Expression): string {
	if (ts.isIdentifier(expr)) return expr.text;
	if (ts.isPropertyAccessExpression(expr)) {
		return `${heritageExprText(expr.expression)}.${expr.name.text}`;
	}
	return expr.getText();
}

function memberNameText(name: ts.PropertyName, sourceFile: ts.SourceFile): string {
	if (ts.isIdentifier(name)) return name.text;
	if (ts.isStringLiteral(name) || ts.isNumericLiteral(name)) return name.text;
	return name.getText(sourceFile);
}

const RESERVED_MODULE_KEYWORDS = new Set(["global"]);

// ---------------------------------------------------------------------------
// Type parameter scope (for distinguishing `typeParam` refs from `ref`s
// without using the type checker: we track which names are bound as type
// parameters of an enclosing declaration).
// ---------------------------------------------------------------------------

class ScopeStack {
	private stack: Set<string>[] = [];

	push(names: string[]) {
		this.stack.push(new Set(names));
	}

	pop() {
		this.stack.pop();
	}

	has(name: string): boolean {
		for (let i = this.stack.length - 1; i >= 0; i--) {
			if (this.stack[i]!.has(name)) return true;
		}
		return false;
	}
}

const scope = new ScopeStack();

function typeParamNames(
	params: ts.NodeArray<ts.TypeParameterDeclaration> | undefined,
): string[] {
	if (!params) return [];
	return params.map((p) => p.name.text);
}

// ---------------------------------------------------------------------------
// Type conversion
// ---------------------------------------------------------------------------

const PRIM_KEYWORDS: Partial<Record<ts.SyntaxKind, string>> = {
	[ts.SyntaxKind.StringKeyword]: "string",
	[ts.SyntaxKind.NumberKeyword]: "number",
	[ts.SyntaxKind.BooleanKeyword]: "boolean",
	[ts.SyntaxKind.BigIntKeyword]: "bigint",
	[ts.SyntaxKind.VoidKeyword]: "void",
	[ts.SyntaxKind.AnyKeyword]: "any",
	[ts.SyntaxKind.UnknownKeyword]: "unknown",
	[ts.SyntaxKind.NullKeyword]: "null",
	[ts.SyntaxKind.UndefinedKeyword]: "undefined",
	[ts.SyntaxKind.NeverKeyword]: "never",
	[ts.SyntaxKind.ObjectKeyword]: "object",
	[ts.SyntaxKind.SymbolKeyword]: "symbol",
};

function convertLiteralType(node: ts.LiteralTypeNode, sourceFile: ts.SourceFile): IRType {
	const lit = node.literal;
	if (lit.kind === ts.SyntaxKind.NullKeyword) {
		return { k: "prim", name: "null" };
	}
	if (lit.kind === ts.SyntaxKind.TrueKeyword) return { k: "literal", value: true };
	if (lit.kind === ts.SyntaxKind.FalseKeyword) return { k: "literal", value: false };
	if (ts.isStringLiteral(lit)) return { k: "literal", value: lit.text };
	if (ts.isNumericLiteral(lit)) return { k: "literal", value: Number(lit.text) };
	if (ts.isBigIntLiteral(lit)) return { k: "literal", value: lit.text };
	if (
		ts.isPrefixUnaryExpression(lit) &&
		lit.operator === ts.SyntaxKind.MinusToken &&
		ts.isNumericLiteral(lit.operand)
	) {
		return { k: "literal", value: -Number(lit.operand.text) };
	}
	return { k: "unsupported", text: node.getText(sourceFile) };
}

function convertType(node: ts.TypeNode | undefined, sourceFile: ts.SourceFile): IRType {
	if (!node) return { k: "prim", name: "any" };

	const prim = PRIM_KEYWORDS[node.kind];
	if (prim) return { k: "prim", name: prim };

	if (ts.isParenthesizedTypeNode(node)) {
		return convertType(node.type, sourceFile);
	}

	if (ts.isLiteralTypeNode(node)) {
		return convertLiteralType(node, sourceFile);
	}

	if (ts.isTypeReferenceNode(node)) {
		const name = entityNameText(node.typeName);
		if (!node.typeArguments && !name.includes(".") && scope.has(name)) {
			return { k: "typeParam", name };
		}
		return {
			k: "ref",
			name,
			args: (node.typeArguments ?? []).map((t) => convertType(t, sourceFile)),
		};
	}

	if (ts.isArrayTypeNode(node)) {
		return { k: "array", elem: convertType(node.elementType, sourceFile) };
	}

	if (ts.isTupleTypeNode(node)) {
		return {
			k: "tuple",
			elems: node.elements.map((e) => {
				if (ts.isNamedTupleMember(e)) return convertType(e.type, sourceFile);
				if (ts.isOptionalTypeNode(e)) return convertType(e.type, sourceFile);
				if (ts.isRestTypeNode(e)) return convertType(e.type, sourceFile);
				return convertType(e as ts.TypeNode, sourceFile);
			}),
		};
	}

	if (ts.isUnionTypeNode(node)) {
		return {
			k: "union",
			types: node.types.map((t) => convertType(t, sourceFile)),
		};
	}

	if (ts.isIntersectionTypeNode(node)) {
		return {
			k: "intersection",
			types: node.types.map((t) => convertType(t, sourceFile)),
		};
	}

	if (ts.isTypeLiteralNode(node)) {
		return {
			k: "object",
			members: node.members
				.map((m) => convertMember(m, sourceFile))
				.filter((m): m is IRMember => m !== undefined),
		};
	}

	if (ts.isFunctionTypeNode(node)) {
		return {
			k: "function",
			params: node.parameters.map((p) => convertParam(p, sourceFile)),
			returns: convertType(node.type, sourceFile),
		};
	}

	// Conditional types, mapped types, typeof, keyof/other type operators,
	// indexed access, template literal types, import types, this-type, etc.
	// all fall through here per spec 1.2.
	return { k: "unsupported", text: node.getText(sourceFile) };
}

function convertParam(node: ts.ParameterDeclaration, sourceFile: ts.SourceFile): IRParam {
	return {
		name: node.name.getText(sourceFile),
		type: convertType(node.type, sourceFile),
		optional: node.questionToken !== undefined || node.initializer !== undefined,
		rest: node.dotDotDotToken !== undefined,
	};
}

function hasModifier(node: ts.HasModifiers, kind: ts.SyntaxKind): boolean {
	return (ts.getModifiers(node) ?? []).some((m) => m.kind === kind);
}

function convertMember(
	node: ts.TypeElement | ts.ClassElement,
	sourceFile: ts.SourceFile,
): IRMember | undefined {
	if (ts.isPropertySignature(node) || ts.isPropertyDeclaration(node)) {
		if (!node.name) return undefined;
		const readonly =
			"modifiers" in node ? hasModifier(node, ts.SyntaxKind.ReadonlyKeyword) : false;
		return {
			kind: "property",
			name: memberNameText(node.name, sourceFile),
			doc: getDocComment(node, sourceFile),
			type: convertType(node.type, sourceFile),
			optional: node.questionToken !== undefined,
			readonly,
		};
	}

	if (ts.isMethodSignature(node) || ts.isMethodDeclaration(node)) {
		if (!node.name) return undefined;
		const typeParams = node.typeParameters?.map((tp) => convertTypeParam(tp, sourceFile)) ?? [];
		scope.push(typeParamNames(node.typeParameters));
		try {
			return {
				kind: "method",
				name: memberNameText(node.name, sourceFile),
				doc: getDocComment(node, sourceFile),
				params: node.parameters.map((p) => convertParam(p, sourceFile)),
				returns: convertType(node.type, sourceFile),
				typeParams,
			};
		} finally {
			scope.pop();
		}
	}

	if (ts.isIndexSignatureDeclaration(node)) {
		const keyParam = node.parameters[0];
		return {
			kind: "index",
			keyType: convertType(keyParam?.type, sourceFile),
			valueType: convertType(node.type, sourceFile),
		};
	}

	if (ts.isGetAccessorDeclaration(node)) {
		if (!node.name) return undefined;
		return {
			kind: "getter",
			name: memberNameText(node.name, sourceFile),
			type: convertType(node.type, sourceFile),
		};
	}

	if (ts.isConstructorDeclaration(node) || ts.isConstructSignatureDeclaration(node)) {
		return {
			kind: "ctor",
			params: node.parameters.map((p) => convertParam(p, sourceFile)),
		};
	}

	// Set accessors, call signatures and other constructs have no IR
	// representation per spec 1.2 and are intentionally dropped.
	return undefined;
}

function convertTypeParam(
	node: ts.TypeParameterDeclaration,
	sourceFile: ts.SourceFile,
): IRTypeParam {
	const out: IRTypeParam = { name: node.name.text };
	if (node.default) {
		out.default = convertType(node.default, sourceFile);
	}
	return out;
}

function convertHeritage(
	clauses: ts.NodeArray<ts.HeritageClause> | undefined,
	sourceFile: ts.SourceFile,
): IRType[] {
	if (!clauses) return [];
	const out: IRType[] = [];
	for (const clause of clauses) {
		for (const t of clause.types) {
			out.push({
				k: "ref",
				name: heritageExprText(t.expression),
				args: (t.typeArguments ?? []).map((a) => convertType(a, sourceFile)),
			});
		}
	}
	return out;
}

// ---------------------------------------------------------------------------
// Declaration collection
// ---------------------------------------------------------------------------

const declOrder: string[] = [];
const declsByKey = new Map<string, IRDecl>();

function declKey(module: string, kind: string, name: string): string {
	return `${module} ${kind} ${name}`;
}

function addDecl(decl: IRDecl) {
	const key = declKey(decl.module as string, decl.kind as string, decl.name as string);
	if (decl.kind === "interface" && declsByKey.has(key)) {
		// Declaration merging: append members to the existing entry.
		const existing = declsByKey.get(key)!;
		const existingMembers = (existing.members as unknown[]) ?? [];
		const newMembers = (decl.members as unknown[]) ?? [];
		existing.members = existingMembers.concat(newMembers);
		if (!existing.doc && decl.doc) existing.doc = decl.doc;
		const existingExtends = (existing.extends as unknown[]) ?? [];
		const newExtends = (decl.extends as unknown[]) ?? [];
		existing.extends = existingExtends.concat(newExtends);
		return;
	}
	declsByKey.set(key, decl);
	declOrder.push(key);
}

function visit(node: ts.Node, sourceFile: ts.SourceFile, module: string, nsPrefix: string) {
	if (ts.isInterfaceDeclaration(node)) {
		const name = nsPrefix ? `${nsPrefix}.${node.name.text}` : node.name.text;
		scope.push(typeParamNames(node.typeParameters));
		try {
			addDecl({
				kind: "interface",
				name,
				module,
				doc: getDocComment(node, sourceFile),
				typeParams: node.typeParameters?.map((tp) => convertTypeParam(tp, sourceFile)) ?? [],
				extends: convertHeritage(node.heritageClauses, sourceFile),
				members: node.members
					.map((m) => convertMember(m, sourceFile))
					.filter((m): m is IRMember => m !== undefined),
			});
		} finally {
			scope.pop();
		}
		return;
	}

	if (ts.isClassDeclaration(node) && node.name) {
		const name = nsPrefix ? `${nsPrefix}.${node.name.text}` : node.name.text;
		scope.push(typeParamNames(node.typeParameters));
		try {
			addDecl({
				kind: "class",
				name,
				module,
				doc: getDocComment(node, sourceFile),
				typeParams: node.typeParameters?.map((tp) => convertTypeParam(tp, sourceFile)) ?? [],
				extends: convertHeritage(node.heritageClauses, sourceFile),
				members: node.members
					.map((m) => convertMember(m, sourceFile))
					.filter((m): m is IRMember => m !== undefined),
			});
		} finally {
			scope.pop();
		}
		return;
	}

	if (ts.isTypeAliasDeclaration(node)) {
		const name = nsPrefix ? `${nsPrefix}.${node.name.text}` : node.name.text;
		scope.push(typeParamNames(node.typeParameters));
		try {
			addDecl({
				kind: "alias",
				name,
				module,
				doc: getDocComment(node, sourceFile),
				typeParams: node.typeParameters?.map((tp) => convertTypeParam(tp, sourceFile)) ?? [],
				type: convertType(node.type, sourceFile),
			});
		} finally {
			scope.pop();
		}
		return;
	}

	if (ts.isModuleDeclaration(node)) {
		const isGlobalAugmentation =
			(node.flags & ts.NodeFlags.GlobalAugmentation) !== 0 ||
			(ts.isIdentifier(node.name) && RESERVED_MODULE_KEYWORDS.has(node.name.text));
		if (!node.body || !ts.isModuleBlock(node.body)) return;

		if (ts.isStringLiteral(node.name)) {
			// declare module "cloudflare:xyz" { ... }
			const newModule = moduleNameText(node.name);
			for (const stmt of node.body.statements) {
				visit(stmt, sourceFile, newModule, nsPrefix);
			}
			return;
		}

		if (isGlobalAugmentation) {
			// declare global { ... } — statements belong to the global scope.
			for (const stmt of node.body.statements) {
				visit(stmt, sourceFile, module, nsPrefix);
			}
			return;
		}

		// declare namespace Foo[.Bar] { ... }
		const nameText = moduleNameText(node.name as ts.Identifier | ts.QualifiedName);
		const newPrefix = nsPrefix ? `${nsPrefix}.${nameText}` : nameText;
		for (const stmt of node.body.statements) {
			visit(stmt, sourceFile, module, newPrefix);
		}
		return;
	}

	// Other top-level statements (declare function/var/const, export
	// statements, enums, etc.) do not map to an IR "kind" per spec 1.2 and
	// are intentionally skipped.
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

function main() {
	const args = parseArgs(process.argv.slice(2));
	const entryPoint = findEntryPoint();

	const text = readFileSync(entryPoint.file, "utf8");
	const sourceFile = ts.createSourceFile(
		entryPoint.file,
		text,
		ts.ScriptTarget.Latest,
		/* setParentNodes */ true,
		ts.ScriptKind.TS,
	);

	for (const stmt of sourceFile.statements) {
		visit(stmt, sourceFile, "", "");
	}

	const decls = declOrder.map((key) => declsByKey.get(key)!);

	const ir = {
		source: {
			package: "@cloudflare/workers-types",
			version: entryPoint.version,
			entry: entryPoint.entry,
		},
		decls,
	};

	const outDir = path.resolve(process.cwd(), args.out);
	mkdirSync(outDir, { recursive: true });
	writeFileSync(
		path.join(outDir, "index.json"),
		`${JSON.stringify(ir, null, 2)}\n`,
	);
	writeFileSync(
		path.join(outDir, "SOURCE"),
		`@cloudflare/workers-types@${entryPoint.version} ${entryPoint.date}\n`,
	);

	console.log(
		`extracted ${decls.length} declarations from ${entryPoint.entry} (v${entryPoint.version}) -> ${outDir}`,
	);
}

main();
