# 产品需求文档：LocalDrop 轻量级局域网同步工具

## 1. 产品概述
**LocalDrop** 是一款基于 Go 语言开发的极简 Web 服务，旨在解决局域网内不同设备（Windows, macOS, Android, iOS）之间文本、图片、浏览器书签和常见文件的快速传递问题。它采用“信息流”模式，支持剪贴板一键同步，并提供自动化的磁盘空间管理。

## 2. 技术栈建议 (最小化占用方案)
* **后端**: Go (Gin 或 Echo 框架) + SQLite (单文件数据库，零配置)。
* **前端**: Vue 3 或 Alpine.js (轻量化) + Tailwind CSS。
* **存储**: 本地文件系统存储图片与文件，SQLite 记录元数据。

---

## 3. 功能需求详情

### 3.1 信息流展示与操作
| 功能 | 描述 |
| :--- | :--- |
| **流式布局** | 采用时间轴倒序排列，最新消息置顶，支持响应式布局（适配手机与电脑）。 |
| **置顶操作** | 支持将重要文本、图片或文件置顶，置顶内容在信息流最上方显示，并有视觉标识。 |
| **删除操作** | 每条信息均有独立的删除按钮，点击后同步删除数据库记录及物理文件。 |
| **文件下载** | 图片和普通文件记录均提供下载入口，并保留原始文件名。 |
| **一键拷贝** | 文本信息下方提供“复制”按钮。针对移动端，需调用 `navigator.clipboard` 接口确保兼容性。 |
| **书签同步** | 支持导入浏览器导出的 `bookmarks.html`，持久化保存标题与链接，并在书签模块中统一浏览。 |

### 3.2 高效上传
* **剪贴板读取**: 网页提供“一键上传剪贴板”按钮。
    * **文本**: 自动获取剪贴板文字并生成记录。
    * **图片**: 检测剪贴板中的图片数据（Blob），直接上传至服务器。
* **拖拽上传**: 支持将本地图片或文件拖入网页区域完成上传。
* **书签导入**: 支持选择浏览器导出的 `bookmarks.html` 文件，服务端会解析其中的 HTTP/HTTPS 书签并全量同步保存。

### 3.3 存储管理与清理
* **磁盘监控**: 网页顶部或侧边栏实时显示当前数据占用的总磁盘空间（含数据库与本地文件目录）。
* **智能清理**: 
    * 提供“**清理一周前图片**”功能按钮。
    * **执行逻辑**: 删除创建时间 > 7 天的所有图片记录及其物理文件，但**保留**所有文本记录。

---

## 4. 交互流程设计

### 4.1 核心操作流程
1.  **上传流程**: 用户点击“同步剪贴板”或选择本地文件 -> JS 获取内容 -> 发送 POST 请求 -> Go 后端写入 SQLite/保存原始文件 -> 前端 WebSocket 或轮询更新流。
2.  **置顶/删除**: 点击按钮 -> 发送 Patch/Delete 请求 -> 后端修改 `is_top` 状态或物理删除 -> 前端局部更新。

### 4.2 UI 界面结构
* **Header**:
    * 当前磁盘占用 (e.g., "Disk Usage: 156MB")
    * [清理一周前图片] 按钮（高亮/红色预警色）
* **Input Area**:
    * [上传剪贴板内容] 大按钮
    * [选择图片] / [选择文件] 按钮
* **Feed Stream**:
    * 卡片式布局（卡片内包含：类型标签、内容预览、时间戳、置顶/拷贝/删除操作区）。

---

## 5. 数据结构设计 (SQLite)

```sql
CREATE TABLE records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content_type TEXT, -- 'text' / 'image' / 'file'
    content_body TEXT, -- 如果是文本则存内容，否则存服务端文件路径
    file_name TEXT DEFAULT '', -- 原始文件名
    mime_type TEXT DEFAULT '', -- MIME 类型
    is_top BOOLEAN DEFAULT 0,
    file_size INTEGER DEFAULT 0, -- 单位: bytes
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 6. 非功能性需求
1.  **极小占用**: 
    * 编译后的二进制文件需控制在 15MB 以内。
    * 静态资源（JS/CSS）建议嵌入到 Go 二进制中（使用 `//go:embed`），实现单文件分发。
2.  **跨端兼容性**: 
    * 必须支持 iOS Safari 和 Android Chrome 的 `navigator.clipboard` 异步 API。
    * 图片预览需支持点击放大。
3.  **网络环境**: 仅限局域网访问，无需账号登录（或支持可选的简易 Pin 码验证）。

---

## 7. 当前实现说明

当前仓库已按上述需求落地为可运行的首版程序：

* **后端**: Go `net/http` + SQLite + 本地文件存储
* **前端**: Vue 3 + Vite + Tailwind CSS
* **发布方式**: 前端构建后嵌入 Go 二进制，生产模式单文件运行
* **实时更新**: 前端每 3 秒短轮询一次

### 7.1 目录结构

```text
cmd/localdrop/        Go 启动入口
internal/localdrop/   配置、存储、HTTP API、测试
web/                  Vue 前端与嵌入资源
scripts/dev.sh        本地开发脚本
Makefile              bootstrap / dev / build / test
```

### 7.2 本地开发

首次准备环境：

```bash
brew install go
make bootstrap
```

启动开发模式：

```bash
make dev
```

开发模式下：

* Go 服务默认监听 `0.0.0.0:8080`
* Vite 开发服务器运行在 `127.0.0.1:5173`
* 前端请求会代理到后端 API

### 7.3 生产构建

```bash
make build
./bin/localdrop
```

默认数据目录为当前项目下的 `data/`，会自动生成：

* `data/localdrop.db`
* `data/images/`

如果需要构建不同平台版本：

```bash
make build-host
make build-android-arm64
make build-all
```

其中 Android arm64（Termux）产物会输出到：

```text
dist/android-arm64-termux/localdrop
```

这个版本面向 64 位 ARM Android 设备，可直接拷贝到 Termux 中执行。
如果本机还没有 Android NDK，可先执行：

```bash
brew install android-ndk
```

### 7.4 测试

```bash
make test
```

已覆盖的自动化测试包括：

* 文本与图片上传
* 普通文件上传与下载
* 置顶排序
* 删除记录时联动删除物理文件
* 清理一周前图片
* 图片文件缺失时的清理容错
* 存储占用统计

### 7.5 可选环境变量

* `LOCALDROP_ADDR`: 服务监听地址，默认 `0.0.0.0:8080`
* `LOCALDROP_DATA_DIR`: 数据目录，默认 `./data`
* `LOCALDROP_MAX_UPLOAD_MB`: 单个上传文件大小上限，默认 `20`
