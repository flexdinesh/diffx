import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { Ajv2020 } from "ajv/dist/2020.js";
import { parse } from "yaml";
import {
  validComment,
  formatComments,
} from "../../apps/web/src/review-comments.ts";
import type {
  ApiFilePatch,
  ApiRepositoryDiff,
  ApiReviewComment,
} from "../../packages/api/src/client.ts";

const schema: unknown = parse(
  readFileSync(
    new URL("../../packages/api/openapi.yaml", import.meta.url),
    "utf8",
  ),
);
assert.ok(typeof schema === "object" && schema !== null);
const ajv = new Ajv2020({ strict: false, validateFormats: false });
ajv.addSchema(schema, "api");
const repository = ajv.compile<ApiRepositoryDiff>({
  $ref: "api#/components/schemas/RepositoryDiff",
});
const filePatch = ajv.compile<ApiFilePatch>({
  $ref: "api#/components/schemas/FilePatch",
});
const reviewComment = ajv.compile<ApiReviewComment>({
  $ref: "api#/components/schemas/ReviewComment",
});
const cases: unknown = JSON.parse(
  readFileSync(new URL("./comments.json", import.meta.url), "utf8"),
);
assert.ok(Array.isArray(cases));
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
for (const value of cases) {
  const entry: unknown = value;
  assert.ok(record(entry));
  test(String(entry.name), () => {
    const comment: unknown = entry.comment;
    assert.equal(validComment(comment), entry.valid);
    // Invalid selections still have a placement; Go verifies that separately.
    if (!validComment(comment)) return;
    const snapshot: unknown = entry.repository;
    assert.ok(snapshot === null || repository(snapshot));
    assert.ok(entry.patch === undefined || filePatch(entry.patch));
    // Placement is server-computed; TypeScript exports the placed comment.
    const placed: unknown = { ...comment, placement: entry.placement };
    assert.ok(reviewComment(placed), JSON.stringify(reviewComment.errors));
    if (typeof entry.export === "string")
      assert.equal(formatComments([placed], true), entry.export);
    if (typeof entry.exportUnresolved === "string")
      assert.equal(formatComments([placed], false), entry.exportUnresolved);
  });
}
