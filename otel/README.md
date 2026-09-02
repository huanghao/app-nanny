# otel（本地可观测性栈，nanny 管理）

本地跑的 [grafana/otel-lgtm](https://github.com/grafana/docker-otel-lgtm) all-in-one 容器：一个镜像里打包了 Grafana + Prometheus + Tempo + Loki + Pyroscope + OpenTelemetry Collector + OBI（eBPF 自动观测），给本地开发中的服务提供 traces/metrics/logs/profiles 的统一查看入口。

**仅本地用**，不是生产可观测性方案。生产场景见公司内部的 mnl-otel（Hulk/Plus 部署，独立于这里）。

**默认不强制接入**：这套栈平时不占用你项目的资源，你的服务不接入也完全不受影响。只有想看某个服务的 trace/log/metric 时才按下面「接入方法」加几行环境变量。

## nanny 怎么管它

跟其他 nanny 项目一样，一个 `app-nanny.toml` + Mode A 单命令：

```bash
cd otel
nanny add .
nanny start otel      # 首次运行会 docker run 创建容器，之后是 docker start
nanny stop  otel       # 优雅停止容器
nanny logs  otel -f    # 看 LGTM 栈自身的启动/运行日志
nanny status otel
```

`run.sh` 里做了判断：容器 `lgtm` 不存在则 `docker run` 创建，存在则 `docker start -a` 启动已有容器（保留 `lgtm-data` volume 里的历史数据，不会每次重建）。停止时不依赖 docker CLI 转发信号给容器（macOS/Docker Desktop 下这个转发不可靠，实测会出现 nanny 认为已停但容器其实还在跑的状态漂移）——`run.sh` 自己 trap SIGTERM，收到后显式跑 `docker stop`，等容器真正退出再让脚本退出。

不设 `autostart`——默认不随 daemon 自动拉起，用的时候自己 `nanny start otel`。

## 入口

| 用途 | 地址 |
|---|---|
| Grafana UI | http://localhost:3080 （默认 admin/admin） |
| OTLP gRPC | localhost:4317 |
| OTLP HTTP | localhost:4318 |
| Tempo API（容器内部端口，宿主机映射） | localhost:3200 |

`nanny logs otel` 里镜像自己打印的 "3000: Grafana (http://localhost:3000)" 说的是**容器内部端口**，跟宿主机映射（`-p 3080:3000`，见 run.sh）无关，不是真实访问地址——真实入口以上表为准。

无鉴权、无网关——本地单机场景，谁都能连 localhost 就行，不要把这些端口暴露到公网/内网其它机器。

## 接入方法（各项目自己改）

### 项目本身被 nanny 管理（推荐）

在项目的 `app-nanny.toml` 里，对应的顶层配置（Mode A）或 `[processes.<name>]`（Mode B）加一行 `otel_service_name`：

```toml
[processes.ts]
command           = "npx tsx index.ts"
otel_service_name = "my-service-ts"
```

nanny 启动这个进程时会自动注入 `OTEL_SERVICE_NAME` / `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_PROTOCOL` 三个标准环境变量（值见 `internal/config/project.go` 的 `LocalOtelEndpoint`），不用在 `command` 里手写、也不用担心端口以后改了要挨个项目改。语言自己的自动埋点入口（Node 的 `NODE_OPTIONS='--import @opentelemetry/auto-instrumentations-node/register'`、Python 的 `opentelemetry-instrument` 前缀）该怎么写还是怎么写，只是不用再手动拼那三个 OTEL_EXPORTER_OTLP_* 了。参考实际例子：`kolab/app-nanny.toml`（ts + py 两个 process 都接了）。

`nanny ps` 会有一列 `OTEL`，显示每个进程声明的 `otel_service_name`（没声明就是 `-`）——这是"声明接入"，见下面「服务发现」一节。

### 项目不受 nanny 管理，或想手动控制

给你的服务加这几个环境变量（换成自己的 `OTEL_SERVICE_NAME`），大多数语言的 OTel SDK 会自动读取：

```bash
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_SERVICE_NAME=my-service
export OTEL_RESOURCE_ATTRIBUTES="deployment.environment.name=local"
```

Python（示例，具体按项目已有依赖调整）：

```bash
pip install opentelemetry-distro opentelemetry-exporter-otlp
opentelemetry-instrument python app.py
```

Node.js：跟着 `@opentelemetry/auto-instrumentations-node` 的 `--require` 用法接同一组环境变量。

Go：用 `go.opentelemetry.io/otel` + `otlptracehttp`/`otlpmetrichttp` exporter，`WithEndpoint("localhost:4318")`。

接入后打开 http://localhost:3080/explore 选对应 datasource（Tempo/Loki/Prometheus/Pyroscope，均已预置好，不用自己配）。

**没有默认脱敏**：这是本地单容器，不像 mnl-otel 有 Collector 层做 Authorization/Cookie/body 剥离。本地环境本来风险就低，但如果服务连了真实第三方密钥或生产数据，自己在埋点前避免把敏感值塞进 trace attribute。

## 服务发现：谁知道谁接入了

三层都可选，互不强依赖——这是设计的核心约束，不是偶然结果：

```
nanny 本体  ──不依赖──>  otel/ 项目
   ▲                        ▲
   │ 声明(toml)              │ 推送(OTLP，失败即丢，不阻塞)
   │                        │
业务项目 ───────可选接入──────┘
```

**1. nanny 知道 otel 的存在，但不依赖它。**
`otel/` 就是普通一个 nanny 项目（`otel/app-nanny.toml`），跟 kolab、md-viewer 地位相同。nanny 核心功能（进程管理、端口、日志）不 import 任何 otel/Grafana 相关代码，唯一的耦合点是 `internal/config.LocalOtelEndpoint` 这个常量——业务进程声明接入时，nanny 用它来注入环境变量。otel 没启动，这个常量还在，只是没人监听那个端口，SDK 的推送请求会直接失败——见下一条。

**2. 业务服务知道自己接入了 otel，靠的是它自己的 `app-nanny.toml`。**
跟它已经知道"自己被 nanny 管理"是同一个机制：这些信息不在某个隐藏配置里，就在项目根目录这一个文件里，agent/人读一遍就知道。`otel_service_name` 字段就是这个声明——出现在某个 process 上，说明"这个进程的目标是把数据发到本地 otel"，跟 otel 此刻是否真的在跑无关（声明的是意图，不是实时状态）。

**3. `otel` 起不起，不该影响业务服务能不能跑。**
这不是 nanny 要去保证的事，是 OTel SDK 的标准行为：exporter 是异步推送（BatchSpanProcessor/PeriodicExportingMetricReader），网络请求失败就在后台丢弃/重试，不会阻塞业务请求路径，也不会让进程在启动时因为连不上 collector 而挂掉。`otel_service_name` 声明 + nanny 注入的环境变量只是告诉 SDK "有地方就往这发"，不建立任何启动时的健康检查或强依赖。kolab 的 ts/py 两个 process 都是这个模式，多次重启 nanny/otel 互不影响对方能不能起来。

**4. nanny 知道哪些受管服务"声明接入"了，但不知道数据是不是真的在流动。**
`nanny ps` 的 `OTEL` 列，读的是所有已注册项目 toml 里的 `otel_service_name`，静态声明，otel 有没有跑都能看到——这一步不需要 otel 处于运行状态，因为它只是读文件，不发网络请求：

```bash
nanny ps    # OTEL 列非 "-" 的就是声明接入的
```

"声明接入" ≠ "数据在流动"：进程可能声明了但从没跑起来过、埋点写错了、或者最近没触发流量。要确认数据真的到了，得反过来问 otel 自己（Grafana 的 datasource proxy 查 Prometheus/Tempo/Loki 有没有这个 service_name 的新鲜数据）——这一步只在 otel 处于运行状态时才有意义，nanny 的 Go 代码里没有内置这个查询（不想让 nanny 核心依赖 Grafana HTTP API），按需手动查：

```bash
# otel 必须是 running 状态（nanny status otel 确认）
curl -s -u admin:admin -G "http://localhost:3080/api/datasources/proxy/uid/prometheus/api/v1/query" \
  --data-urlencode 'query=count by(__name__)({service_name="my-service"})'
```

一句话总结这四层的边界：**声明是静态的（读 toml，随时可查），数据流动是动态的（问 otel，otel 得先起来）**——两者故意不合并成一个状态，因为合并了就意味着 nanny 的 `ps` 输出会依赖 otel 是否在跑，破坏了"otel 只是可选项目"这条设计前提。

## 数据规模控制（不让它把本地拖垮）

本地持续写入，不加限制的话数据只会一直涨。四个存储后端都已经在 `run.sh` 里配了约 7 天的保留窗口，稳定运行后磁盘占用会趋于稳定（老数据被后端自己的 compactor/retention 机制清掉），不用人工定期清理：

| 后端 | 怎么限的 | 备注 |
|---|---|---|
| Prometheus | `PROMETHEUS_EXTRA_ARGS`：`--storage.tsdb.retention.time=7d --storage.tsdb.retention.size=2GB` | 时间和大小任一超限先触发 |
| Loki | `config/loki-config.yaml` 里加的 `compactor`/`limits_config`（镜像默认**完全没配 retention**，不加这个会一直攒） | 镜像默认没有 `compactor:` 段，必须整段自己补，改完用 `-verify-config` 校验过 |
| Pyroscope | `PYROSCOPE_EXTRA_ARGS`：`-retention-period=168h` | — |
| Tempo | 不管，用镜像自带默认值（`tempo --help` 确认 `block-retention` 默认 `336h0m0s`，即 14 天） | Tempo 3.x 的 config schema 里已经没有旧版 `compactor:` 顶层字段了，硬加会导致解析报错、容器起不来（踩过一次坑，见下）；14 天本身就是够用的下限，没必要为了改到 7 天冒这个险 |

Prometheus/Pyroscope 用的是启动参数（`*_EXTRA_ARGS`），Loki 用的是整份配置文件覆盖（镜像里没暴露对应 CLI flag），两种机制不一样，改的时候留意别混。

**踩过的坑**：第一版想把 Tempo 也按同样思路加 `compactor: compaction: block_retention: 168h` 到配置文件，结果容器起不来，日志是 `Error: Tempo exited before becoming ready`，nanny 因为 `restart = on-failure` 一直重试到 `max_restarts` 才停下（`nanny status otel` 会显示 `crashed`）。单独拉起 Tempo 二进制排查才看到根因：`failed parsing config: ... field compactor not found in type app.Config`——这个镜像版本的 Tempo（3.0.3）配置结构已经变了，不能照抄旧文档里的写法。教训：**改这类 all-in-one 镜像的组件配置前，先用 `docker run --rm --entrypoint <bin> <image> -config.file=... -verify-config`（Loki 支持）或单独起一下这个组件（Tempo 不支持 `-verify-config`，只能真跑一次看报错）单独验证，不要直接改完就让 nanny 拉起整个栈去踩** ——整栈是一个 Mode A 进程，任何一个组件配置错都会让 `run-all.sh` 整体退出，殃及其它已经工作正常的组件。

**手动检查磁盘占用**（无论有没有触发 retention，想确认现状随时可以看）：

```bash
docker exec lgtm du -sh /data
```

**镜像更新**：`docker pull grafana/otel-lgtm:latest`，然后 `nanny stop otel && docker rm lgtm && nanny start otel`（会用新镜像重建容器，`lgtm-data` volume 数据保留，`run.sh` 里的 mount/参数照常生效）。

**还是想彻底清空重来**：`nanny stop otel && docker rm lgtm && docker volume rm lgtm-data`（**丢历史数据**，下次 `nanny start otel` 重新创建空容器）。

**完全卸载**：`nanny stop otel && nanny remove otel && docker rm lgtm && docker volume rm lgtm-data`。

## 已知偏差 / 历史

这个容器最初是手动 `docker run` 起的（未纳入 nanny 前），曾设置 Docker 自身的 `--restart unless-stopped`。纳入 nanny 管理后已改为 `--restart no`（`docker update --restart=no lgtm`），避免 Docker daemon 重启时绕过 nanny 把容器拉起来，导致 nanny 里显示 stopped 但容器其实在跑的状态不一致。现在容器的启停完全由 `nanny start/stop otel` 控制。
