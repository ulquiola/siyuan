import * as assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {ModuleKind, transpileModule} from "typescript";

const languages = {
    tailscaleRunning: "已连接", tailscaleNeedsLogin: "需要登录", tailscaleStarting: "正在连接",
    tailscaleError: "错误", tailscaleDisabled: "未启用",
    tailscaleWaitingAuth: "正在等待登录链接，请检查网络或代理", tailscaleOpenLogin: "打开登录链接",
};
const status = (state: string, authURL = "", error = "") => ({state, authURL, ips: null as string[] | null, error});

const load = () => {
    const exports = {} as typeof import("./tailscaleStatus");
    const document = {
        createElement: (tag: string) => ({
            tagName: tag.toUpperCase(), className: "", href: "", target: "", rel: "", textContent: "",
        }),
        createTextNode: (text: string) => ({nodeValue: text}),
    };
    runInNewContext(transpileModule(readFileSync("src/config/tabs/tailscaleStatus.ts", "utf8"), {
        compilerOptions: {module: ModuleKind.CommonJS},
    }).outputText, {exports, document});
    return exports;
};

test("未连接且有认证链接时给出链接", () => {
    const url = "https://login.tailscale.com/a/1";
    const starting = load().describeTailscaleStatus(status("starting", url), languages);
    assert.equal(starting.text, "正在连接");
    assert.equal(starting.loginURL, url);
    const needsLogin = load().describeTailscaleStatus(status("needsLogin", url), languages);
    assert.equal(needsLogin.text, "需要登录");
    assert.equal(needsLogin.loginURL, url);
});

test("已连接时不给出链接，哪怕后端残留 authURL", () => {
    const ret = load().describeTailscaleStatus(status("running", "https://login.tailscale.com/a/2"), languages);
    assert.equal(ret.text, "已连接");
    assert.equal(ret.loginURL, "");
});

test("已连接时把 Tailnet 地址附在文案后", () => {
    const ret = load().describeTailscaleStatus({state: "running", authURL: "", ips: ["100.64.0.5"], error: ""}, languages);
    assert.equal(ret.text, "已连接 · 100.64.0.5");
});

test("拿不到链接时给出等待提示", () => {
    assert.equal(load().describeTailscaleStatus(status("starting"), languages).text,
        "正在连接 · 正在等待登录链接，请检查网络或代理");
    assert.equal(load().describeTailscaleStatus(status("needsLogin"), languages).text,
        "需要登录 · 正在等待登录链接，请检查网络或代理");
});

test("错误与未启用状态不叠加等待提示", () => {
    assert.equal(load().describeTailscaleStatus(status("error", "", "listen failed"), languages).text,
        "错误: listen failed");
    assert.equal(load().describeTailscaleStatus(status("disabled"), languages).text, "未启用");
});

test("applyTailscaleStatus 渲染链接且重复调用不累积", () => {
    const api = load();
    const element = {
        _text: "", children: [] as unknown[],
        get textContent() { return this._text; },
        set textContent(value: string) { this._text = value; this.children.length = 0; },
        appendChild(child: unknown) { this.children.push(child); return child; },
    };
    const url = "https://login.tailscale.com/a/3";
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("needsLogin", url), languages);
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("needsLogin", url), languages);
    assert.equal(element.textContent, "需要登录");
    assert.equal(element.children.length, 2);
    const link = element.children[1] as {tagName: string; className: string; href: string; target: string; rel: string};
    assert.deepEqual([link.tagName, link.className, link.href, link.target, link.rel],
        ["A", "fn__a", url, "_blank", "noreferrer"]);
    assert.equal((link as unknown as {textContent: string}).textContent, "打开登录链接");
});
