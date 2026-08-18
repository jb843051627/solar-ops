# solar-ops Docker 打包说明

## 基础镜像

`golang:1.22-bookworm`（与 go.mod 语言版本 1.22 对齐）

## 环境变量

- `GOPROXY=https://goproxy.cn,direct`（国内代理）
- `GOTOOLCHAIN=local`（钉死工具链，防自动切换）

## 构建

```bash
# amd64
./build_benzhi_docker.sh solar-ops-base linux/amd64

# arm64 (QEMU 模拟)
./build_benzhi_docker.sh solar-ops-base linux/arm64
```

## 运行

```bash
docker run --rm -e SOLAR_OPS_DB=/data/solar-ops.db -p 8080:8080 benzhi/solar-ops-base:latest
```
