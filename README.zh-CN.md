# grdpwasm

[English](README.md) | 中文说明

基于 Go WebAssembly 与 [grdp](https://github.com/nakagami/grdp) 构建的网页版 RDP 客户端。
在浏览器里直连 Windows 远程桌面——无需任何插件。

> **本仓库是 [nakagami/grdpwasm](https://github.com/nakagami/grdpwasm) 的 `aiti-aigrdpwasm` fork**，
> 在上游之上带有一个补丁版 grdp vendor fork 及多层功能/修复，详见下方
> [本 fork 更新说明](#本-fork-更新说明)。
> 已发布为 npm 包 [`@aiti-ai/grdpwasm`](https://www.npmjs.com/package/@aiti-ai/grdpwasm)。

## 本 fork 更新说明

### xrdp / gnome-remote-desktop 兼容

- 内置补丁版 grdp fork（`third_party/grdp`）：planar codec 常量修复；能力集广播裁剪到本客户端能真正
  支持的范围（不广播 SURFACE_COMMANDS、frame marker、bitmap-cache REV2）；NSCodec 捕获检测用于
  服务器类型判别；全屏刷新恢复初始画面损伤；ECDSA CredSSP 公钥回显；fast-path 输入 PDU 8 字节对齐；
  NLA 失败返回明确错误信息。
- EGFX 自动协商：先带 GFX 连接；若服务器实际编码本客户端无法解码（xrdp 只发 progressive/planar），
  自动以兼容模式重连一次。GDS/Windows（H.264）正常连接，无感知。
- 无损 WASM 传输层：收包队列既不阻塞 JS 线程也不丢消息（丢包 = 流失步 → 花屏/断连）；突发期内存
  由桥的背压机制约束有界。

### 前端

- **fixedTarget 模式**：目标 host/port 由服务端提供（注入 `__rdpTarget`）或经 `?fixedTarget=true` 传入，
  前端不可篡改；standalone 模式保留自由连接表单。
- 虚拟键盘（程序员布局）、快捷键条、原生系统键盘代理、X11/RFB keysym 工具——载体无关公共组件。
- xrdp 彩色光标缓解：AND 掩码导致整个矩形全不透明的光标会渲染成黑块，检测到（不透明占比 ≥95%）即丢弃。

### 打包与分发（@aiti-ai/grdpwasm）

- 已发布为 npm 包。tarball 里带 `main.wasm`（jsDelivr/unpkg 分发的源头），但 postinstall 在安装后即删除
  本地副本——运行时页面经 [justget](https://github.com/justdb/justget) 从 CDN 竞速下载 wasm，同源兜底
  （覆盖方式：`?wasm=<url>` 或 `localStorage['grdpwasm.wasmUrl']`）。
- 面向嵌入方的服务端兜底：后端可按 `ensureRdpWasm` 模式把 wasm 预取到缓存目录，静态服务随时有得 serve。

## 架构

```
浏览器 (WASM) ──WebSocket──► proxy (Go) ──TCP──► RDP 服务器
```

浏览器无法直接开 TCP socket，由一个轻量 Go 代理把浏览器的 WebSocket 连接桥接到
RDP 服务器的 TCP 端口。

## 环境要求

- Go 1.24 或更高
- 一台可达的 RDP 服务器（Windows 或任何 RDP 兼容主机）

## 构建

```bash
git clone https://github.com/nakagami/grdpwasm.git
cd grdpwasm
make all
```

`make all` 产物：

| 产物 | 说明 |
|---|---|
| `static/main.wasm` | Go WASM 二进制（跑在浏览器里） |
| `static/wasm_exec.js` | Go 运行时 JS 支撑文件 |
| `proxy/proxy` | WebSocket↔TCP 代理 + 静态文件服务 |

## 运行

```bash
make serve
# 或者等价手动启动：
./proxy/proxy -listen :8080 -static static
```

然后用浏览器打开 **http://localhost:8080**。

### 代理参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-listen` | `:8080` | 监听地址与端口 |
| `-static` | `static` | 静态文件目录 |

## 使用

1. 浏览器打开 `http://localhost:8080`。
2. 填写连接表单：
   - **Host** — RDP 服务器主机名或 IP
   - **Port** — RDP 端口（默认 `3389`）
   - **Domain** — Windows 域（本地账号留空）
   - **User** — 用户名
   - **Password** — 密码
   - **Width / Height** — 初始桌面分辨率
3. 点击 **Connect**。
4. 远程桌面出现在画布里；点击画布捕获键盘焦点。
5. 点击 **Disconnect** 结束会话。

## 键盘与鼠标

所有标准键盘输入经 RDP 扫描码转发到远程桌面。鼠标移动、按键点击、滚轮完整支持。

> **注意：** 浏览器标签页必须持有焦点，键盘事件才会转发。按键无响应时请点击画布区域。

## 音频

远程音频经 RDPSND 流式传输，通过浏览器 Web Audio API 播放
（PCM 44100 Hz、立体声、16 位有符号小端）。

## 安全须知

- 代理接受任意来源的连接。请只在可信网络运行，或先加认证再暴露到公网。
- 凭据经 WebSocket 从浏览器传到代理。跨不可信网络访问时请使用 HTTPS/WSS
  （把代理放在 nginx/Caddy 等 TLS 终结反代之后）。

## 开发

```
npm install     # 安装 JS 依赖（justget、esbuild）
make wasm       # 只重编 WASM 二进制
make proxy      # 只重编代理服务
make wasm_exec  # 用本地 Go 工具链刷新 wasm_exec.js
npm run build:vendor  # 从已安装的 justget 重编 static/vendor/justget.browser.js
make clean      # 清理全部构建产物
```

## npm 包

把前端资产发布为 `@aiti-ai/grdpwasm`。消费者装到除 `main.wasm` 外的全部文件——
页面运行时自行从 CDN 拉取。

```bash
npm run build:vendor   # 把 justget/browser 打包进 static/vendor/
make all               # 构建 main.wasm + wasm_exec.js + proxy
npm publish            # prepack 会把版本号同步进 index.html；tarball 带 wasm
```

安装方拿到的是不含 `main.wasm` 的 `static/`（html/js/vendor）——页面运行时从 CDN 拉取。
如需保留本地副本，安装前设置 `GRDPWASM_KEEP_WASM=1`。

## 许可证

GPLv3 —— 见 [grdp LICENSE](https://github.com/nakagami/grdp/blob/master/LICENSE)。
