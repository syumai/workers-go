// Command mixed is a smoke test for the forwarding mirror described in
// https://github.com/syumai/workers/issues/173: a project that imports both
// the mirror module (old import path) and the source module (new import
// path) in the same build must be able to pass values between them, proving
// that every exported type in the mirror is a true alias of the
// corresponding type in the source module rather than a copy.
//
// internal/cmd/genforward/mirror-tests/run.sh builds this module against a locally generated mirror
// and the source module checkout via "replace" directives, so the import
// paths below are the real mirror and source module paths.
package main

import (
	newkv "github.com/syumai/workers-go/cloudflare/kv"
	newr2 "github.com/syumai/workers-go/cloudflare/r2"
	oldkv "github.com/syumai/workers/cloudflare/kv"
	oldr2 "github.com/syumai/workers/cloudflare/r2"
)

// takesNewNamespace is typed against the new (source module) path.
func takesNewNamespace(ns *newkv.Namespace) *newkv.Namespace { return ns }

// takesOldNamespace is typed against the old (mirror module) path.
func takesOldNamespace(ns *oldkv.Namespace) *oldkv.Namespace { return ns }

// takesNewBucket / takesOldBucket do the same for cloudflare/r2.
func takesNewBucket(b *newr2.Bucket) *newr2.Bucket { return b }
func takesOldBucket(b *oldr2.Bucket) *oldr2.Bucket { return b }

func main() {
	// A *kv.Namespace obtained via the OLD path must be usable wherever a
	// *kv.Namespace from the NEW path is expected, and vice versa. This only
	// type-checks if kv.Namespace is a genuine alias (`type Namespace =
	// src.Namespace`) rather than a distinct defined type.
	var viaOld *oldkv.Namespace
	var viaNew *newkv.Namespace

	viaOld = takesNewNamespace(viaOld) // old -> new -> old
	viaNew = takesOldNamespace(viaNew) // new -> old -> new
	_ = viaOld
	_ = viaNew

	var oldBucket *oldr2.Bucket
	var newBucket *newr2.Bucket

	oldBucket = takesNewBucket(oldBucket)
	newBucket = takesOldBucket(newBucket)
	_ = oldBucket
	_ = newBucket
}
