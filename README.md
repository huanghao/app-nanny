# nanny

本地开发服务管理器。把散落在多个项目目录里的 dev server 统一托管：后台运行、日志捕获、端口冲突检测、web 控制台一键查看。

## 解决的问题

本地同时开发多个项目时，常见的麻烦：

- 不同项目用了相同端口，启动时冲突
- 记不清哪个服务跑在哪个端口
- 每个服务占一个终端窗口，切换混乱
- 服务扔到后台跑，错误日志看不到
- 重开终端后忘了哪些服务还没启动

nanny 的做法：每个项目放一个 `app-nanny.toml` 声明端口和启动命令，daemon 统一管理所有服务，日志写文件，web 控制台随时查看。

## 安装

需要 Go 1.22+，`just` 命令行工具。

```bash
git clone <this-repo>
cd app-nanny
just install       # 编译并安装到 ~/bin/nanny
nanny install      # 可选：写入 launchd，登录后自动启动 daemon
```

## 快速开始

```bash
# 1. 在项目目录放 app-nanny.toml（见下方配置说明）
cd ~/workspace/my-project

# 2. 注册项目
nanny add .

# 3. 启动 daemon（第一次需要）
nanny daemon start

# 4. 启动服务
nanny start my-project

# 5. 查看状态
nanny ps

# 6. 打开 web 控制台
nanny dashboard
```

---

## app-nanny.toml 配置

在**项目根目录**放 `app-nanny.toml`，描述如何启动这个项目。

### Mode A：单命令（最简单）

适合只有一个进程的项目（纯后端、单页应用等）：

```toml
name    = "my-server"
command = ".venv/bin/uvicorn main:app --reload --port ${PORT:-8000}"
restart = "on-failure"

[ports]
PORT = 8000          # 注入为环境变量，命令里用 ${PORT} 读取
```

nanny 启动时执行 `PORT=8000 .venv/bin/uvicorn main:app ...`，并在 `ps` 输出里展示端口。

### Mode B：多进程（前后端分离）

适合同一项目需要同时启动多个进程：

```toml
name    = "my-app"
restart = "on-failure"

[processes.backend]
command = ".venv/bin/uvicorn main:app --reload --port ${PORT:-3010}"
port    = 3010

[processes.frontend]
command     = "npm run dev -- --port ${PORT:-3011}"
working_dir = "frontend"   # 相对项目根目录，此进程在 frontend/ 子目录下运行
port        = 3011
```

每个 `[processes.<name>]` 是一个独立进程，有自己的日志、端口、状态。

### 所有配置项

| 字段 | 位置 | 说明 | 默认值 |
|---|---|---|---|
| `name` | 顶层 | 项目名，唯一标识，用于所有命令 | 必填 |
| `command` | 顶层 (Mode A) | 启动命令，在项目根目录执行 | `"just dev"` |
| `restart` | 顶层 | 崩溃后重启策略：`"on-failure"` / `"always"` / `"never"` | `"on-failure"` |
| `max_restarts` | 顶层 | 最多重启次数，防止无限循环 | `5` |
| `autostart` | 顶层 | daemon 启动时自动拉起此服务 | `false` |
| `[ports]` | Mode A | 环境变量名 → 端口号，启动时注入 | — |
| `[processes.<name>]` | Mode B | 独立进程定义 | — |
| `command` | Mode B 进程 | 该进程的启动命令 | 必填 |
| `port` | Mode B 进程 | 声明端口，冲突检测 + 注入为 `PORT=<port>` | — |
| `working_dir` | Mode B 进程 | 相对项目根目录的工作目录 | 项目根目录 |
| `memory_warn_mb` | Mode B 进程 | 内存超限告警阈值（MB） | — |

### 自定义错误检测规则

```toml
[[error_patterns]]
match = "CRITICAL"    # 日志行包含此字符串时触发错误事件
```

内置规则已覆盖：HTTP 5xx、Python Traceback、`Error:`、`panic:`、`FATAL` 等。

---

## 常用命令

```bash
# 项目管理
nanny add [dir]              # 注册项目（读取当前目录或指定目录的 app-nanny.toml）
nanny remove <name>          # 取消注册

# 服务控制
nanny start  <name>[/process]   # 启动（幂等，已运行的进程自动跳过）
nanny stop   <name>[/process]   # 停止
nanny restart <name>[/process]  # 重启（重新读取 toml，配置变更立即生效）

# 状态查看
nanny ps                     # 所有服务一览（端口 / 状态 / 内存 / uptime）
nanny status <name>          # 某服务详细信息（含 cwd、错误数）

# 日志
nanny logs <name>[/process] [-f] [-n 100]   # 查看日志，-f 实时追踪
nanny errors <name> [--last] [--copy]        # 查看错误事件

# Daemon
nanny daemon start/stop/status   # 管理后台 daemon
nanny install / uninstall        # launchd 自动启动（macOS）

# Web 控制台
nanny dashboard              # 打开 http://localhost:7070
nanny version                # 显示 CLI 版本
```

> **注意**：`nanny daemon stop` 只停止 daemon 进程，不会 kill 已启动的服务。服务会继续运行，daemon 重启后自动重新接管（adopted）。要停止服务请用 `nanny stop <name>`。

## Web 控制台

`nanny dashboard` 打开 `http://localhost:7070`，提供：

- 所有服务的状态、端口、内存、uptime 总览
- 点击行查看实时日志（SSE 流式推送）
- 出现 500 / Error / panic 时行上显示红色错误计数，点击确认
- start / stop / restart 按钮
- 日志面板高度可拖拽，记录在 localStorage

## 端口约定（个人偏好）

```
3000        单进程项目（如 md-viewer）
3010/3011   第一个前后端项目（backend=3010, frontend=3011）
3020/3021   第二个
3030        ...
```

## 日志文件

所有日志写入 `~/.local/share/app-nanny/logs/`：

```
parquet-explorer-backend.log
parquet-explorer-frontend.log
md-viewer-server.log
```

单文件上限 50MB，保留 3 个备份文件，共约 150MB/服务。
