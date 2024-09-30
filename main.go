package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var (
	openAIURL *url.URL
	logger    *log.Logger
	config    struct {
		OpenAIAPIURL        string            `json:"openai_api_url"`
		OpenAIAPIKey        string            `json:"openai_api_key"`
		PromptModifications map[string]string `json:"prompt_modifications"`
	}
)

func init() {
	// 读取配置文件
	configFile, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal("读取配置文件时出错:", err)
	}

	err = json.Unmarshal(configFile, &config)
	if err != nil {
		log.Fatal("解析配置文件时出错:", err)
	}

	// 解析OpenAI API URL
	openAIURL, err = url.Parse(config.OpenAIAPIURL)
	if err != nil {
		log.Fatal("解析OpenAI API URL时出错:", err)
	}

	// 初始化日志记录器
	logFile, err := os.OpenFile("requests.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal("打开日志文件时出错:", err)
	}
	logger = log.New(logFile, "", log.LstdFlags)
}

func main() {
	r := gin.Default()

	// 配置CORS中间件
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = []string{"http://localhost:5173"} // 指定允许的源
	corsConfig.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	corsConfig.ExposeHeaders = []string{"Content-Length"}
	corsConfig.AllowCredentials = true // 允许凭证
	r.Use(cors.New(corsConfig))

	// 允许所有HTTP方法
	r.Any("/*path", handleProxy)

	log.Println("服务器启动，监听端口 :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal("启动服务器时出错:", err)
	}
}

func handleProxy(c *gin.Context) {
	logger.Printf("收到请求: %s %s", c.Request.Method, c.Request.URL.Path)
	logger.Printf("请求头: %v", c.Request.Header)

	// 创建反向代理
	proxy := httputil.NewSingleHostReverseProxy(openAIURL)

	// 修改请求
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = openAIURL.Host

		// 使用用户传过来的 API Key
		userAPIKey := c.GetHeader("Authorization")
		if userAPIKey != "" {
			req.Header.Set("Authorization", userAPIKey)
		} else {
			// 如果用户没有提供 API Key，则使用配置文件中的 Key
			req.Header.Set("Authorization", "Bearer "+config.OpenAIAPIKey)
		}

		// 不再手动删除CORS头，以避免重复
	}

	// 设置 ModifyResponse 仅用于日志记录，不处理CORS
	proxy.ModifyResponse = logResponse

	// 捕获并修改请求体
	var requestBody []byte
	var err error
	if c.Request.Body != nil {
		requestBody, err = io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Printf("读取请求体时出错: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "内部服务器错误"})
			return
		}
		modifiedBody := modifyRequestBody(requestBody)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(modifiedBody))
		c.Request.ContentLength = int64(len(modifiedBody))
	}

	// 记录请求
	logger.Printf("请求: %s %s\n请求体: %s", c.Request.Method, c.Request.URL.Path, string(requestBody))

	// 处理 OPTIONS 请求
	if c.Request.Method == "OPTIONS" {
		// CORS中间件已经处理了CORS头，这里只需返回200即可
		c.Status(http.StatusOK)
		return
	}

	// 检查是否为流式请求
	isStreamRequest := strings.Contains(c.Request.URL.Path, "/stream") || (c.Request.Header.Get("Accept") == "text/event-stream")

	if isStreamRequest {
		handleStreamRequest(c, proxy)
	} else {
		handleNonStreamRequest(c, proxy)
	}
}

func handleStreamRequest(c *gin.Context, proxy *httputil.ReverseProxy) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")
	// 不再手动设置CORS头

	responseWriter := c.Writer
	responseWriter.WriteHeader(http.StatusOK)

	proxyWriter := &streamResponseWriter{
		ResponseWriter: responseWriter,
		logger:         logger,
	}

	proxy.ServeHTTP(proxyWriter, c.Request)
}

type streamResponseWriter struct {
	gin.ResponseWriter
	logger *log.Logger
}

func (w *streamResponseWriter) Write(p []byte) (int, error) {
	lines := bytes.Split(p, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		trimmedLine := bytes.TrimPrefix(line, []byte("data: "))
		if bytes.Equal(trimmedLine, []byte("[DONE]")) {
			w.logger.Printf("流式传输结束")
			continue
		}
		var chunk map[string]interface{}
		err := json.Unmarshal(trimmedLine, &chunk)
		if err != nil {
			w.logger.Printf("解析流式数据时出错: %v", err)
			continue
		}
		w.logger.Printf("流式数据块: %s", string(trimmedLine))
	}
	return w.ResponseWriter.Write(p)
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseBodyWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func modifyRequestBody(body []byte) []byte {
	var request map[string]interface{}
	err := json.Unmarshal(body, &request)
	if err != nil {
		logger.Printf("解析请求体时出错: %v", err)
		return body
	}

	if messages, ok := request["messages"].([]interface{}); ok {
		for i, msg := range messages {
			if message, ok := msg.(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					// 记录原始提示词
					logger.Printf("原始提示词: %s", content)

					// 修改提示词
					originalContent := content
					for keyword, replacement := range config.PromptModifications {
						content = strings.ReplaceAll(content, keyword, replacement)
					}

					// 只有在内容被修改时才记录修改后的提示词
					if content != originalContent {
						logger.Printf("修改后的提示词: %s", content)
					}

					message["content"] = content
					messages[i] = message
				}
			}
		}
		request["messages"] = messages
	}

	modifiedBody, err := json.Marshal(request)
	if err != nil {
		logger.Printf("序列化修改后的请求体时出错: %v", err)
		return body
	}

	return modifiedBody
}

func logResponse(resp *http.Response) error {
	// 删除上游响应中的CORS头，以避免与Gin的CORS中间件冲突
	resp.Header.Del("Access-Control-Allow-Origin")
	resp.Header.Del("Access-Control-Allow-Methods")
	resp.Header.Del("Access-Control-Allow-Headers")

	if resp.Header.Get("Content-Type") == "text/event-stream" {
		logger.Printf("流式响应开始: %d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Printf("读取响应体时出错: %v", err)
		return err
	}
	resp.Body = io.NopCloser(bytes.NewBuffer(body))

	logger.Printf("响应: %d\n响应体: %s", resp.StatusCode, string(body))
	return nil
}

func handleNonStreamRequest(c *gin.Context, proxy *httputil.ReverseProxy) {
	responseWriter := &responseBodyWriter{
		ResponseWriter: c.Writer,
		body:           &bytes.Buffer{},
	}

	proxy.ServeHTTP(responseWriter, c.Request)

	body := responseWriter.body.Bytes()
	logger.Printf("响应: %d\n响应体: %s", responseWriter.statusCode, string(body))

	// 解析响应体以提取AI的回复
	var response map[string]interface{}
	err := json.Unmarshal(body, &response)
	if err == nil {
		if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					if content, ok := message["content"].(string); ok {
						logger.Printf("AI 回复: %s", content)
					}
				}
			}
		}
	} else {
		logger.Printf("解析AI回复时出错: %v", err)
	}

	// 依赖于CORS中间件设置CORS头，无需在这里手动设置
	c.Data(responseWriter.statusCode, responseWriter.Header().Get("Content-Type"), body)
}
