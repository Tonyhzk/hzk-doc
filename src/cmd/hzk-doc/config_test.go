package main

import (
	"io"
	"testing"
	"time"
)

func TestOptions(t *testing.T) {
	cfg, err := parseOptions([]string{"--docs", "./资料", "--listen", "0.0.0.0:0", "--name", "团队知识库", "--refresh", "5s"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.docs != "./资料" || cfg.listen != "0.0.0.0:0" || cfg.name != "团队知识库" || cfg.refresh != 5*time.Second {
		t.Fatalf("参数未正确传入: %+v", cfg)
	}
	defaults, err := parseOptions(nil, io.Discard)
	if err != nil || defaults.listen != "127.0.0.1:0" || defaults.refresh != 3*time.Second {
		t.Fatalf("默认参数错误: %+v, %v", defaults, err)
	}
	version, err := parseOptions([]string{"--version"}, io.Discard)
	if err != nil || !version.showVersion {
		t.Fatal("版本参数没有生效")
	}
}

func TestInvalidOptions(t *testing.T) {
	cases := [][]string{
		{"--refresh", "0s"}, {"--refresh", "500ms"}, {"--refresh", "bad"},
		{"--docs", ""}, {"--listen", ""}, {"--name", "  "},
		{"unexpected"}, {"--unknown"},
	}
	for _, args := range cases {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Errorf("无效参数未被拒绝: %v", args)
		}
	}
}
