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

## 维护

- **镜像更新**：`docker pull grafana/otel-lgtm:latest`，然后 `nanny stop otel && docker rm lgtm && nanny start otel`（会用新镜像重建容器，`lgtm-data` volume 数据保留）。
- **磁盘占用**：数据在 named volume `lgtm-data`，实测 5 天写入量约 450MB。镜像默认配置没有显式设置 retention（用各组件默认值），本地长期挂着会持续增长。定期看一眼：`docker exec lgtm du -sh /data`；占用过大就整体清空：`nanny stop otel && docker rm lgtm && docker volume rm lgtm-data`（**丢历史数据**，下次 `nanny start otel` 会重新创建空容器）。
- **完全卸载**：`nanny stop otel && nanny remove otel && docker rm lgtm && docker volume rm lgtm-data`。

## 已知偏差 / 历史

这个容器最初是手动 `docker run` 起的（未纳入 nanny 前），曾设置 Docker 自身的 `--restart unless-stopped`。纳入 nanny 管理后已改为 `--restart no`（`docker update --restart=no lgtm`），避免 Docker daemon 重启时绕过 nanny 把容器拉起来，导致 nanny 里显示 stopped 但容器其实在跑的状态不一致。现在容器的启停完全由 `nanny start/stop otel` 控制。
