package main

import (
	"errors"
	"flag"
	"io"
	"strings"
	"time"
)

type options struct {
	docs        string
	listen      string
	name        string
	refresh     time.Duration
	showVersion bool
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var cfg options
	flags := flag.NewFlagSet("hzk-doc", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.docs, "docs", "./docs", "文档目录")
	flags.StringVar(&cfg.listen, "listen", "127.0.0.1:0", "监听地址，端口 0 表示自动选择")
	flags.StringVar(&cfg.name, "name", "hzk-doc", "站点名称")
	flags.DurationVar(&cfg.refresh, "refresh", 3*time.Second, "外部文件变更检查间隔")
	flags.BoolVar(&cfg.showVersion, "version", false, "显示版本后退出")
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	if flags.NArg() != 0 {
		return cfg, errors.New("不接受位置参数，请使用 --docs 指定文档目录")
	}
	if cfg.showVersion {
		return cfg, nil
	}
	if strings.TrimSpace(cfg.docs) == "" || strings.TrimSpace(cfg.listen) == "" {
		return cfg, errors.New("文档目录与监听地址不能为空")
	}
	if strings.TrimSpace(cfg.name) == "" || len([]rune(cfg.name)) > 120 {
		return cfg, errors.New("站点名称需为 1 到 120 个字符")
	}
	if cfg.refresh < time.Second {
		return cfg, errors.New("文件检查间隔不能小于 1 秒")
	}
	return cfg, nil
}
