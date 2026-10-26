package server

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentReadRequests(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "guide.md", []byte("# 指南\n\n并发搜索关键词。"))
	server := newTestServer(t, root, nil)
	guideID := documentIDFor(t, server, "guide.md")

	targets := []string{
		"/api/tree",
		"/api/doc?id=" + guideID,
		"/api/search?q=关键词&scope=full",
	}
	var wait sync.WaitGroup
	for worker := 0; worker < 12; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 20; iteration++ {
				for _, target := range targets {
					recorder := request(server, http.MethodGet, target)
					if recorder.Code != http.StatusOK {
						t.Errorf("%s 状态码 = %d", target, recorder.Code)
					}
				}
			}
		}()
	}
	wait.Wait()
}

func TestCloseWaitsForRefreshLoop(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "manual.pdf", []byte("%PDF-1.4"))
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	server := newTestServerWithOptions(t, root, func(config *Config) {
		config.RefreshInterval = time.Millisecond
	}, func(options *newOptions) {
		options.pdfExtractor = func(string) (string, error) {
			if calls.Add(1) > 1 {
				close(started)
				<-release
			}
			return "PDF 正文", nil
		}
	})

	writeTestFile(t, root, "change.md", []byte("变更"))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("刷新循环未启动")
	}

	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()

	select {
	case err := <-closed:
		t.Fatalf("刷新循环仍在运行时 Close 提前返回: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("关闭服务失败: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close 未在刷新循环退出后返回")
	}
}
