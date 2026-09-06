// Package cloudflare is the root of the generated Cloudflare Workers
// runtime bindings tree (exp/cloudflare/<pkg>). Each subpackage is
// mechanically derived from the @cloudflare/workers-types .d.ts source by
// cfgen (scripts/gen-bindings/cfgen): one JS interface/class maps to one Go
// type, one method maps to one method. See README.md in this directory for
// the full mapping rules, the regeneration workflow, and how to write an
// overrides file for a new package.
//
// Everything under exp/, including this tree, is experimental: because its
// shape follows workers-types directly, it may gain, lose, or change
// fields and methods in a minor version release of this module as
// workers-types itself changes. Prefer the hand-written idiomatic packages
// under cloudflare/ (kv, r2, d1, queues, sockets, ...) where one exists;
// use exp/cloudflare/<pkg> directly for APIs that don't have one yet, or
// via its JSValue() escape hatch when the generated wrapper doesn't cover
// something you need.
package cloudflare

// WorkersTypesVersion is the exact @cloudflare/workers-types version the
// generated bindings in this tree were derived from. It is bumped whenever
// exp/internal/gen/ir/index.json is regenerated from a newer release.
const WorkersTypesVersion = "5.20260906.1"

//go:generate make -C ../.. gen-bindings
