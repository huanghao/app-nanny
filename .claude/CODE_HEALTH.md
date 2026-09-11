# CODE_HEALTH

活清单：只放当前仍然成立的问题。修复或条目不再适用时整段删除，不保留“已修复”标记。

## 2026-09-11 otel run.sh 镜像版本未固定，与 loki-config.yaml 的注释假设不一致

- 位置：`otel/run.sh:28`（`IMAGE=grafana/otel-lgtm`，无 tag，等价于 `:latest`），`otel/config/loki-config.yaml:5`（注释「Keep the base section in sync when bumping LOKI_VERSION in run.sh」）
- 问题：`run.sh` 里的镜像引用没有固定版本号，实际拉取的是 `:latest`；但 `loki-config.yaml` 的注释暗示存在一个会被"bump"的 `LOKI_VERSION`/镜像版本变量，而 run.sh 里并没有这个变量，一旦上游镜像更新（config schema 变化，README 里就记录过 Tempo 3.x 曾经因为 schema 改动导致启动失败的真实事故），`docker pull` + 重建容器会静默换到新版本，行为可能漂移且难以复现。
- 建议：要么把 `IMAGE` 固定成一个具体版本 tag（如 `grafana/otel-lgtm:v0.31.0`，与 loki-config.yaml 头部注释里提到的版本对齐），升级时手动改这一处；要么把注释改成如实描述"镜像走 `:latest`，升级前自行核对 schema 是否变化"，去掉暗示存在版本钉住机制的措辞。两者都不动代码逻辑，只是澄清/固定这一个字符串。
- 风险：是否固定版本是使用者的取舍（固定版本更可控但需要手动维护升级，`:latest` 省心但可能重演 README 里记录过的 Tempo schema 破坏性事故），偏好性决策，不代为拍板。
