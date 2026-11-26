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
            getAttribute(name: string) { return "href" === name ? this.href : ""; },
        }),
        createTextNode: (text: string) => ({nodeType: 3, nodeValue: text}),
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

test("错误状态下也给出认证链接", () => {
    const url = "https://login.tailscale.com/a/9";
    const ret = load().describeTailscaleStatus(status("error", url, "listen failed"), languages);
    assert.equal(ret.text, "错误: listen failed");
    assert.equal(ret.loginURL, url);
});

test("已连接时不叠加错误文案", () => {
    const ret = load().describeTailscaleStatus(status("running", "", "stale error"), languages);
    assert.equal(ret.text, "已连接");
    assert.equal(ret.loginURL, "");
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

test("未连接时展示控制面失败的真实原因", () => {
    const ret = load().describeTailscaleStatus(status("starting", "", "proxyconnect tcp: connection refused"), languages);
    assert.equal(ret.text,
        "正在连接 · 错误: proxyconnect tcp: connection refused · 正在等待登录链接，请检查网络或代理");
    assert.equal(ret.loginURL, "");
});

test("已连接时不展示任何错误", () => {
    const ret = load().describeTailscaleStatus(status("running", "", "proxyconnect tcp: connection refused"), languages);
    assert.equal(ret.text, "已连接");
});

// 仅供本文件使用的极简 DOM：只模拟 applyTailscaleStatus 用到的节点与查询能力
const makeElement = () => {
    type FakeChild = {tagName?: string; nodeValue?: string; href?: string; textContent?: string};
    const element = {
        _text: "", _textNode: null as {nodeType: number; nodeValue: string} | null,
        children: [] as FakeChild[],
        get textContent() { return this._text; },
        // 真实 DOM 中给 textContent 赋值会替换全部子节点，因此用 _textNode 表示元素自身的文本节点，
        // children 只保留随后追加的子节点
        set textContent(value: string) {
            this._text = value;
            this._textNode = {nodeType: 3, nodeValue: value};
            this.children.length = 0;
        },
        get firstChild() {
            return this._textNode || (this.children.length ? this.children[0] : null);
        },
        querySelector(selector: string) {
            return this.children.find((child: FakeChild) => selector.toUpperCase() === child.tagName) || null;
        },
        appendChild(child: FakeChild) { this.children.push(child); return child; },
    };
    return element;
};

test("applyTailscaleStatus 渲染链接且重复调用不累积", () => {
    const api = load();
    const element = makeElement();
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

test("applyTailscaleStatus 状态不变时复用同一个链接节点", () => {
    const api = load();
    const element = makeElement();
    const url = "https://login.tailscale.com/a/5";
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("needsLogin", url), languages);
    const first = element.children[1];
    assert.equal((first as unknown as {href: string}).href, url);
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("needsLogin", url), languages);
    assert.equal(element.children.length, 2);
    assert.equal(element.children[1], first);
    assert.equal(element.textContent, "需要登录");
});

test("applyTailscaleStatus 认证链接变化时重建链接", () => {
    const api = load();
    const element = makeElement();
    api.applyTailscaleStatus(element as unknown as HTMLElement,
        status("needsLogin", "https://login.tailscale.com/a/6"), languages);
    const first = element.children[1];
    const next = "https://login.tailscale.com/a/7";
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("needsLogin", next), languages);
    assert.equal(element.children.length, 2);
    assert.notEqual(element.children[1], first);
    assert.equal((element.children[1] as unknown as {href: string}).href, next);
});

test("applyTailscaleStatus 连接成功后移除链接", () => {
    const api = load();
    const element = makeElement();
    api.applyTailscaleStatus(element as unknown as HTMLElement,
        status("needsLogin", "https://login.tailscale.com/a/8"), languages);
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("running"), languages);
    assert.equal(element.querySelector("a"), null);
    assert.equal(element.textContent, "已连接");
    assert.equal(element.firstChild?.nodeValue, "已连接");
});
