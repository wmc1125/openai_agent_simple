# 使用官方Go镜像作为构建环境
FROM golang:1.20-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制go mod和sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# 使用轻量级的alpine镜像作为运行环境
FROM alpine:latest  

# 安装ca-certificates，这可能在进行HTTPS请求时需要
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# 从builder阶段复制编译好的执行文件
COPY --from=builder /app/main .
# 复制配置文件
COPY config.json .

# 暴露8080端口
EXPOSE 8081

# 运行应用
CMD ["./main"]