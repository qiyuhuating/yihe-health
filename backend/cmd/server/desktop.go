// Tauri/Rust 桌面进程与 Go sidecar 之间的本地进程协议。
//
// Rust 主进程通过环境变量启动后端：
//
//	YIHE_PORT / YIHE_BIND_ADDRESS  监听地址（PORT=0 表示随机端口）
//	YIHE_READY_FILE                   就绪后原子写入 {"port":N,"pid":M}
//	YIHE_PARENT_PID                   父进程退出时后端自行退出
//	YIHE_DATA_DIR                     数据目录（图片默认落到其下 images/）
//	YIHE_DESKTOP_TOKEN                 桌面端 bootstrap token
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// applyDesktopEnv 把桌面端环境变量折叠进监听地址与图片目录，返回就绪文件与父 PID。
func applyDesktopEnv(addr string) (newAddr, readyFile string, parentPid int) {
	newAddr = addr
	if p := strings.TrimSpace(os.Getenv("YIHE_PORT")); p != "" {
		bind := strings.TrimSpace(os.Getenv("YIHE_BIND_ADDRESS"))
		if bind == "" {
			bind = "127.0.0.1"
		}
		newAddr = net.JoinHostPort(strings.Trim(bind, "[]"), p)
	}
	readyFile = os.Getenv("YIHE_READY_FILE")
	if s := strings.TrimSpace(os.Getenv("YIHE_PARENT_PID")); s != "" {
		if pid, err := strconv.Atoi(s); err == nil && pid > 0 {
			parentPid = pid
		}
	}
	return newAddr, readyFile, parentPid
}

// writeReadyFile 使用同目录临时文件和 rename 发布就绪信息。
func writeReadyFile(path string, port int) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("readyfile: mkdir %s: %v", dir, err)
		return
	}
	tmp, err := os.CreateTemp(dir, "ready-*.tmp")
	if err != nil {
		log.Printf("readyfile: %v", err)
		return
	}
	tmpName := tmp.Name()
	if _, err := fmt.Fprintf(tmp, "{\"port\":%d,\"pid\":%d}", port, os.Getpid()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		log.Printf("readyfile: write: %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		log.Printf("readyfile: %v", err)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		log.Printf("readyfile: %v", err)
	}
}

// watchParent 在桌面父进程退出后关闭 Go 服务。
func watchParent(parentPid int, shutdown func()) {
	if parentPid <= 0 || parentPid == os.Getpid() {
		return
	}
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			if !parentAlive(parentPid) {
				log.Printf("desktop parent %d exited, shutting down", parentPid)
				shutdown()
				return
			}
		}
	}()
}
