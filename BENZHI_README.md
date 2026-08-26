基于 Go 实现的寒区学校蒸压加气混凝土砌块质量闭环 Web 项目，一款后端服务，实现从料浆配制、蒸压检测、异常复验到砌筑准入裁定的全程追溯与并发仲裁。

# aac-block-masonry-admission-closure

本 Git 项目来自模型完成任务后的 workspace，不包含嵌套 .git 记录或本地构建产物。

## 本地构建与测试

```bash
go mod download
go build ./...
go test ./...
./run_benzhi_smoke.sh
```

## Docker 构建与运行

```bash
docker build --platform linux/amd64 -t aac-block-masonry-admission-closure:latest .
./build_benzhi_docker.sh aac-block-masonry-admission-closure linux/arm64
docker run --rm -it --platform linux/arm64 aac-block-masonry-admission-closure:latest
./build_benzhi_docker.sh aac-block-masonry-admission-closure linux/amd64
docker run --rm -it --platform linux/amd64 aac-block-masonry-admission-closure:latest
```

构建脚本第二个参数为目标平台，必须分别完成 linux/arm64 和 linux/amd64 构建与容器验证；未提供时按照规范默认使用 linux/amd64。系统 backend-v2 模板通过 Go 原生交叉编译生成目标架构的 /usr/local/bin/benzhi-app，镜像默认直接运行该入口。
