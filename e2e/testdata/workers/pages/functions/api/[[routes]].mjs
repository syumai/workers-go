// [[routes]].mjs matches every request under /api/* and re-exports the
// generated worker's onRequest handler, following the same pattern as
// _templates/cloudflare/pages-tinygo/functions/api/[[routes]].mjs.
import worker from "../../build/worker.mjs";

export const onRequest = worker.onRequest;
