# OpenAI API 反向代理

这是一个用Go语言编写的OpenAI API反向代理服务。它可以转发POST请求到OpenAI API,根据配置修改提示词,并记录所有的请求、回复和AI响应,包括支持流式请求和响应。

## 功能

- 转发POST请求到OpenAI API
- 根据配置自动修改提示词
- 记录所有请求和响应
- 单独记录AI的回复
- 支持流式请求和响应
- 增强的错误处理和日志记录
- 打印原始和修改后的提示词

## 配置

在项目根目录下创建一个 `config.json` 文件,内容如下:

## 使用

您可以向 `http://localhost:8080` 发送与OpenAI API相同的POST请求,包括流式请求。您可以在请求头中包含自己的 API Key，或者使用配置文件中的默认 Key。

### 使用自定义 API Key

在发送请求时，在请求头中包含 `Authorization` 字段：



## 部署
docker-compose up -d

docker-compose logs -f app

docker-compose down