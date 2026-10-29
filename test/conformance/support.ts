import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createServer } from "node:net";
import { Ajv2020 } from "ajv/dist/2020.js";
import { parse } from "yaml";

const contract: unknown = parse(
  readFileSync(
    new URL("../../packages/api/openapi.yaml", import.meta.url),
    "utf8",
  ),
);
assert.ok(typeof contract === "object" && contract !== null);
const validator = new Ajv2020({ strict: false, validateFormats: false });
validator.addSchema(contract, "api");

/** Asserts that a real HTTP response value matches an OpenAPI schema. */
export function conforms(name: string, value: unknown) {
  const check = validator.compile({ $ref: "api#/components/schemas/" + name });
  assert.ok(check(value), JSON.stringify(check.errors));
}

export async function freePort() {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") {
    server.close();
    throw new Error("Unable to reserve port");
  }
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return address.port;
}

export async function waitForServer(
  url: string,
  headers: Record<string, string> = {},
) {
  let lastError: unknown;
  for (let attempt = 0; attempt < 200; attempt++) {
    try {
      const response = await fetch(`${url}/api/v2/bootstrap`, { headers });
      if (response.ok) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("Go server did not start", { cause: lastError });
}
