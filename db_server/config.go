package main

import (
	"strings"

	"roisoft.com/hdlj/common/config"
)

var (
	cfg_file = "./db_server.cfg"

	cfg_node_addr = "0.0.0.0"
	cfg_node_port = "40000"
	cfg_node_id   = "hdlj.db.0"
	cfg_node_name = "hdlj.db.0"
	cfg_node_tags = []string{"hdlj.db.cluster"}

	cfg_log_custom_level = "1"
	cfg_log_dir          = "./log"

	cfg_db_dir = "./hdlj.db"

	cfg_node_id_suffix = 0
)

var gMapConfig map[string]string

func init() {
	gMapConfig = make(map[string]string)
}

func LoadConfig(f string) (bool, string) {

	cfg, err := config.ReadDefault(f)
	if err != nil {
		return false, "Fail to read config file. " + err.Error()
	}
	for _, sec := range cfg.Sections() {
		opts, _ := cfg.SectionOptions(sec)
		for _, opt := range opts {
			gMapConfig[string(sec+"."+opt)], _ = cfg.String(sec, opt)
		}
	}

	// 把配置读到变量里
	if str, ok := gMapConfig["server.node_addr"]; ok {
		cfg_node_addr = str
	}
	if str, ok := gMapConfig["server.node_port"]; ok {
		cfg_node_port = str
	}
	if str, ok := gMapConfig["server.node_id"]; ok {
		cfg_node_id = str
	}
	if str, ok := gMapConfig["server.node_name"]; ok {
		cfg_node_name = str
	}
	if str, ok := gMapConfig["server.node_tags"]; ok {
		cfg_node_tags = strings.Split(str, ",")
	}
	if str, ok := gMapConfig["server.db_dir"]; ok {
		cfg_db_dir = str
	}
	if str, ok := gMapConfig["server.log_dir"]; ok {
		cfg_log_dir = str
	}
	if str, ok := gMapConfig["server.log_custom_level"]; ok {
		cfg_log_custom_level = str
	}

	return true, ""
}
