# RAgent Server

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