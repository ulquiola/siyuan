import * as assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {ModuleKind, transpileModule} from "typescript";

const load = () => {
    const exports = {} as typeof import("./tailscaleSyncNetwork");
    runInNewContext(transpileModule(readFileSync("src/config/tabs/tailscaleSyncNetwork.ts", "utf8"), {
        compilerOptions: {module: ModuleKind.CommonJS},
    }).outputText, {exports});
    return exports;
};

test("normalizeSyncNetwork 只接受 proxy，其余回退 direct", () => {
    const {normalizeSyncNetwork} = load();
    assert.equal(normalizeSyncNetwork("proxy"), "proxy");
    assert.equal(normalizeSyncNetwork("direct"), "direct");
    assert.equal(normalizeSyncNetwork(""), "direct");
    assert.equal(normalizeSyncNetwork(undefined), "direct");
    assert.equal(normalizeSyncNetwork("bogus"), "direct");
});