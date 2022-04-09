# RAgent Server

## 简介
二进制传输,压缩流量,支持TLS，自动重连，自动切换，client与多对多

## Init Environment

配置环境变量, 配置文件地址和日志文件路径, RAGETNT_CONFIG_PATH 要精确到文件名,  RAGETNT_LOG_DIR只需要目录

```
export RAGETNT_CONFIG_PATH = ""
export RAGETNT_LOG_DIR = ""
```

配置文件示例在 ./config/dev/config.dev.yaml 根据实际需求更改后移到 RAGETNT_CONFIG_PATH 目录

## Build 

```
go build 
```

## RUN

```
./main
```
