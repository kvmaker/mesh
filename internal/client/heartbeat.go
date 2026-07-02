package client

import (
	"fmt"
	"net/http"
	"time"
)

// StartHeartbeat 启动后台心跳 goroutine，定期发送心跳到服务器
func StartHeartbeat(serverAddr, secret string) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		// 立即发送第一次
		sendHeartbeat(serverAddr, secret)

		for range ticker.C {
			sendHeartbeat(serverAddr, secret)
		}
	}()
}

// sendHeartbeat 发送单次心跳请求，错误时静默忽略
func sendHeartbeat(serverAddr, secret string) {
	url := fmt.Sprintf("http://%s/api/devices/heartbeat", serverAddr)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	http.DefaultClient.Do(req)
}
