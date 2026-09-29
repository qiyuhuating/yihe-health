/* 用途：独立核对演示凭据摘要与登录接受、拒绝路径，只依赖 Node 内置模块。
 * check_credentials.js — 凭据配置一致性校验（纯 Node，无需浏览器）
 *
 * 为什么需要它
 * ------------
 * 2026-09-26 把演示凭据从各模块集中到 脚本/config.js 之后，存在一种
 * 静默失败的风险：有人改了 config.js 里的 hash，却没同步 salt（或反之），
 * 结果是**所有人都登不进管理端**，而 node --check 与静态检查都不会报错。
 *
 * 因此这里做两件事：
 *   1. 用 Node 自带 crypto 独立复算 SHA-256(salt + ":" + 口令)，
 *      与 config.js 里的常量比对 —— 不依赖被测代码的校验实现；
 *   2. 真实加载 demo-credentials.js + admin-credentials.js，
 *      跑通"正确口令通过 / 错误口令拒绝 / 错误账号拒绝"三条路径。
 *
 * 退出码 0 = 通过，1 = 失败。
 */
"use strict";
const fs = require("fs");
const path = require("path");
const vm = require("vm");
const nodeCrypto = require("crypto");

const ROOT = path.resolve(__dirname, "..");
const failures = [];

function read(rel) {
    return fs.readFileSync(path.join(ROOT, rel), "utf8");
}

/* ---- 1. 独立复算摘要（不经过被测代码）---- */
const sandbox = {window: {}};
sandbox.window.window = sandbox.window;
vm.createContext(sandbox);
vm.runInContext(read("脚本/config.js"), sandbox, {filename: "脚本/config.js"});
const C = sandbox.window.YIHE_CONFIG;

const expectations = [
    ["adminHash", C.adminSalt, "admin123", "管理端默认口令 admin123"],
    ["userHash", C.userSalt, "123456", "居民端默认口令 123456"],
];
for (const [key, salt, password, label] of expectations) {
    const actual = nodeCrypto.createHash("sha256").update(salt + ":" + password).digest("hex");
    if (actual !== C[key]) {
        failures.push(`${label}: SHA-256(${salt}:${password}) = ${actual}\n      但 config.js 的 ${key} = ${C[key]}`);
    } else {
        console.log(`  [OK] ${label}: ${key} 与独立复算一致`);
    }
}

/* ---- 2. 真实走一遍凭据模块 ---- */
const win = {console};
win.window = win;
win.crypto = nodeCrypto.webcrypto;
win.TextEncoder = TextEncoder;
vm.createContext(win);
for (const f of ["脚本/config.js", "脚本/demo-credentials.js", "脚本/admin-credentials.js"]) {
    vm.runInContext(read(f), win, {filename: f});
}

const cases = [
    ["admin", "admin123", true, "正确口令应通过"],
    ["admin", "wrong", false, "错误口令应拒绝"],
    ["intruder", "admin123", false, "错误账号应拒绝"],
    ["admin", "", false, "空口令应拒绝"],
];

(async () => {
    for (const [user, password, expected, label] of cases) {
        const r = await win.AdminCredentials.verify(user, password);
        if (r.ok !== expected) {
            failures.push(`${label}: AdminCredentials.verify(${user}) -> ok=${r.ok}（mode=${r.mode}），期望 ${expected}`);
        } else {
            console.log(`  [OK] ${label}`);
        }
    }

    if (failures.length) {
        console.error("\n凭据配置校验失败：");
        failures.forEach((f) => console.error("  " + f));
        process.exit(1);
    }
    console.log("\nPASS: 凭据配置一致性校验通过（摘要可复算 + 校验路径正确）");
})();
