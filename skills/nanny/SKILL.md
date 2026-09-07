---
name: nanny
description: 当需要在本地启动、停止、查看开发服务时使用。如果项目目录下有 app-nanny.toml，或者用户提到 nanny、本地服务管理、查看某个服务的日志/状态时激活。
---

`nanny` 是本机的本地开发服务管理器，负责统一启动、停止和监控所有 dev server。服务以 daemon 形式后台运行，日志写文件，重启 daemon 不会杀掉服务。

## 快速定位

```bash
nanny ps                        # 看所有服务状态（端口/状态/最近活跃时间）
nanny daemon status             # daemon 是否在跑
nanny logs <project>[/process]  # 看日志（支持 -f 实时跟）
nanny errors <project> --last   # 最近一次错误事件 + traceback
nanny status <project>          # 某服务详情（cwd/内存/错误数）
```

## 服务生命周期

```bash
nanny daemon start              # 启动后台 daemon（首次或重启后）
nanny start <project>           # 启动服务（幂等，已运行的自动跳过）
nanny start <project>/<process> # 只启动某个子进程
nanny stop  <project>           # 停止
nanny restart <project>         # 重启（会重新读取 toml，配置变更立即生效）
nanny add [dir]                 # 注册项目（读取当前目录的 app-nanny.toml）
nanny remove <project>          # 取消注册
```

daemon stop 不会杀死服务，只停止管理进程本身。

## app-nanny.toml 配置

每个项目根目录放一个 `app-nanny.toml`。

### Mode A：单进程

```toml
name    = "my-server"
command = ".venv/bin/uvicorn main:app --reload --port ${PORT:-3030}"
restart = "on-failure"

[ports]
PORT = 3030
```

顶层字段必须放在 `[ports]` 之前。`${PORT:-3030}` 表示优先使用 nanny 注入的环境变量，默认 3030。

### Mode B：多进程（前后端分离）

```toml
name    = "my-app"
restart = "on-failure"

[processes.backend]
command = "uvicorn main:app --reload --port ${PORT:-3020}"
port    = 3020

[processes.frontend]
command     = "npm run dev -- --port ${PORT:-3021}"
working_dir = "frontend"   # 相对项目根目录
port        = 3021
```

Mode B 每个进程有独立日志、独立状态，可以单独重启。

### 所有配置项

| 字段 | 说明 |
|---|---|
| `name` | 项目名，唯一标识 |
| `command` | 启动命令（Mode A，默认 `"just dev"`） |
| `restart` | `"on-failure"` / `"always"` / `"never"` |
| `max_restarts` | 最多重启次数（默认 5） |
| `autostart` | daemon 启动时自动拉起（默认 false） |
| `[ports]` | Mode A 的环境变量注入，键=变量名，值=端口号 |
| `[processes.<name>]` | Mode B 子进程，有 command/port/working_dir/memory_warn_mb/otel_service_name |
| `otel_service_name` | 接入本地 otel（见 otel/README.md），自动注入 OTEL_* 环境变量；`nanny ps` 的 OTEL 列显示声明情况 |

## 本地可观测性栈（otel/）

`otel/` 是 nanny 管理的一个本地 Grafana/Prometheus/Tempo/Loki/Pyroscope 容器（`nanny start otel`），默认不启动、不强制任何项目接入。项目要接入，在 `app-nanny.toml` 里加 `otel_service_name`（见上表），nanny 会自动注入三个 OTEL_EXPORTER_OTLP_* 环境变量。谁声明了接入看 `nanny ps` 的 OTEL 列；谁真的有数据在流动、端口/维护/卸载方法，看 `otel/README.md`（里面有「服务发现」一节讲清楚 nanny/otel/业务服务三者怎么互不强依赖）。

## 端口规律（本机约定）

前端端口 = 后台端口 + 1。不同应用从 3000 开始，每个 +10。

**查当前已用端口，直接跑：**

```bash
nanny ps
```

输出里的 PORTS 列即为已声明端口。找到最大的 3x10 基数，新项目用 +10。

例如已有 3000、3010、3020、3030，下一个后台端口就是 3040（前端 3041）。

## 日志

```bash
nanny logs parquet-explorer           # 聚合所有子进程（加 [backend] [frontend] 前缀）
nanny logs parquet-explorer/backend   # 单进程
nanny logs parquet-explorer -f        # 实时跟（-f 只支持单进程，聚合模式会提示）
```

日志文件位置：`~/.local/share/app-nanny/logs/<project>-<process>.log`

重启时日志里会有分割线：`────────── restarted at HH:MM:SS ──────────`

### 项目自己开文件写的日志（nanny 没 spawn 的进程）

上面这条日志是 nanny 捕获被它拉起的进程的 stdout/stderr，自动按 50MB×3 轮转，接入 nanny 的项目不用管这部分。

但有些项目还有 nanny 管不到的产出——不是 `command`/`[processes.*]` 里配的那个进程写的，而是项目自己的其他组件（配套的 GUI app、被 Chrome/其他程序拉起的辅助进程等），nanny 从没 spawn 过它们，天然看不到、也捕获不到它们的输出。这类组件如果自己 `os.OpenFile` 打日志，历史上就是没人管、一直涨（例如 context-pad 的 macOS 面板进程 `panel.log` 曾经涨到 390M）。

**约定**：这类文件写到 `~/.local/share/app-nanny/logs/<project>/` 这个子目录下，文件名随意（`panel.log`、`switches.log` 都行，nanny 不关心具体名字）。`nanny gc` 会认这个位置：目录本身对应的项目还在 `registry.json` 里的话，目录内任何单个文件超过 50MB 就原地截断保留最近 10MB（不区分是不是 nanny 自己写的）；项目已经 `nanny remove` 掉了，整个子目录直接删。

新项目接入 nanny、且有这类"nanny 管不到的进程也要写日志"的情况时，把日志路径改成这个约定位置即可，不用额外找 nanny 配置或注册——gc 只看这个路径本身,不需要声明。

## Web 控制台

```bash
nanny dashboard    # 打开 http://localhost:7070
```

提供服务列表、实时日志（点击行）、start/stop/restart 按钮、500 错误红色徽章。

## 常见操作流程

**在新项目里接入 nanny：**
1. 在项目根目录写 `app-nanny.toml`（参考上面的 Mode A/B 示例）
2. `nanny add .`
3. `nanny start <name>`

**排查服务异常：**
1. `nanny ps` 看状态
2. `nanny errors <name> --last` 看最近错误
3. `nanny logs <name> -f` 实时看

**改了配置让它生效：**
直接 `nanny restart <name>`，每次 start/restart 都会重新读取 toml。
