package main

import (
	"io"
	"os"
	"path/filepath"
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

func TestPasswordFileOption(t *testing.T) {
	cfg, err := parseOptions([]string{"--admin-password-file", "./admin-password"}, io.Discard)
	if err != nil || cfg.passwordFile != "./admin-password" {
		t.Fatalf("密码文件参数错误: %+v, %v", cfg, err)
	}
	if _, err := parseOptions([]string{"--admin-password-file"}, io.Discard); err == nil {
		t.Fatal("缺少密码文件参数未被拒绝")
	}
}

func TestReadAdminPassword(t *testing.T) {
	cases := []struct {
		name     string
		env      string
		contents string
		file     bool
		missing  bool
		want     string
		wantErr  bool
	}{
		{name: "disabled"},
		{name: "environment", env: "environment-password", want: "environment-password"},
		{name: "short-environment", env: "short", wantErr: true},
		{name: "file", file: true, contents: "file-password-123", want: "file-password-123"},
		{name: "file-priority", file: true, env: "environment-password", contents: "file-password-123", want: "file-password-123"},
		{name: "unix-newline", file: true, contents: "file-password-123\n", want: "file-password-123"},
		{name: "windows-newline", file: true, contents: "file-password-123\r\n", want: "file-password-123"},
		{name: "preserve-spaces", file: true, contents: " file-password-123 \n", want: " file-password-123 "},
		{name: "empty-file", file: true, env: "environment-password", contents: "\n", wantErr: true},
		{name: "short-file", file: true, contents: "short", wantErr: true},
		{name: "unicode-length", file: true, contents: "密码不能只按字节数", wantErr: true},
		{name: "missing-file", file: true, missing: true, env: "environment-password", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HZK_DOC_ADMIN_PASSWORD", tc.env)
			filename := ""
			if tc.file {
				filename = filepath.Join(t.TempDir(), "admin-password")
				if !tc.missing {
					if err := os.WriteFile(filename, []byte(tc.contents), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			got, err := readAdminPassword(filename)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("密码读取结果不符合预期，error=%v", err)
			}
		})
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
