//go:generate protoc -I ../proto --go_out=plugins=grpc:../proto/ ../proto/server.proto ../proto/client.proto ../proto/common.proto  ../proto/db.proto ../proto/db_roisoft_acct.proto ../proto/db_hdlj.proto
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/golang/glog"
	"roisoft.com/hdlj/common"
)

func init() {
	flag.String("cfg", cfg_file, "configuration file")
	flag.String("node_id", cfg_node_id, "node id")
	flag.String("node_name", cfg_node_name, "node name")
	flag.String("node_addr", cfg_node_addr, "ip")
	flag.String("node_port", cfg_node_port, "port")
	flag.String("db_dir", cfg_db_dir, "database file path")
}

func load_config() (bool, string) {

	// 检查命令行里是否存在cfg,log_dir,v参数
	log_dir, vmodule := "", ""
	for i, v := range os.Args {
		if strings.HasPrefix(v, "-cfg") {
			if len(v) > 4 && v[4] == '=' {
				cfg_file = v[5:]
			} else if len(os.Args) > i+1 {
				cfg_file = os.Args[i+1]
			}
		} else if strings.HasPrefix(v, "-log_dir") {
			if len(v) > 8 && v[8] == '=' {
				log_dir = v[9:]
			} else if len(os.Args) > i+1 {
				log_dir = os.Args[i+1]
			}
		} else if strings.HasPrefix(v, "-v") {
			if len(v) > 2 && v[2] == '=' {
				vmodule = v[3:]
			} else if len(os.Args) > i+1 {
				vmodule = os.Args[i+1]
			}
		} else if strings.HasPrefix(v, "-vmodule") {
			if len(v) > 8 && v[8] == '=' {
				vmodule = v[9:]
			} else if len(os.Args) > i+1 {
				vmodule = os.Args[i+1]
			}
		}
	}
	cfg_file = common.GetAbsFilePath(cfg_file)
	// 读取配置文件的参数
	if ok, err := LoadConfig(cfg_file); !ok {
		return false, fmt.Sprintf("read config(%s) error: %s\n", cfg_file, err)
	}

	// 设置命令行参数
	if len(log_dir) > 0 {
		cfg_log_dir = log_dir
	}
	cfg_log_dir = common.GetAbsFilePath(cfg_log_dir)
	flag.Set("log_dir", cfg_log_dir)
	if len(vmodule) > 0 {
		cfg_log_custom_level = vmodule
	}
	flag.Set("v", cfg_log_custom_level)
	// 解析命令行参数
	flag.Parse()
	// 读取命令行参数
	if f := flag.Lookup("node_id"); f != nil {
		cfg_node_id = f.Value.String()
	}
	if f := flag.Lookup("node_name"); f != nil {
		cfg_node_name = f.Value.String()
	}
	if f := flag.Lookup("node_addr"); f != nil {
		cfg_node_addr = f.Value.String()
	}
	if f := flag.Lookup("node_port"); f != nil {
		cfg_node_port = f.Value.String()
	}
	if f := flag.Lookup("db_dir"); f != nil {
		cfg_db_dir = f.Value.String()
	}

	// 取node_id最后部分作为数据根目录
	pos := strings.LastIndex(cfg_node_id, ".")
	if pos == -1 {
		pos = 0
	} else {
		pos++
	}

	if n, err := strconv.Atoi(cfg_node_id[pos:]); err != nil {
		return false, "node_id is not a number."
	} else if n < 0 || n >= (1<<16) {
		return false, "node_id is between 0-65535."
	} else {
		cfg_node_id_suffix = n
	}

	return true, ""
}

func main() {
	// get config from file
	if ok, err := load_config(); !ok {
		fmt.Println(err)
		return
	}

	os.MkdirAll(cfg_log_dir, 0777)
	fmt.Println("for detail, log in file (", cfg_log_dir, ")")

	defer glog.Flush()
	defer glog.V(common.Log_Info_Level_1).Infof("stop server. [node_id = %s]\n", cfg_node_id)
	glog.V(common.Log_Info_Level_1).Infof("start server. [node_id = %s]\n", cfg_node_id)

	server_addr := fmt.Sprintf("%s:%s", cfg_node_addr, cfg_node_port)
	db_service := NewDbService()
	if ok, err := db_service.start_grpc_service(server_addr); !ok {
		glog.Errorln(err)
		return
	}

	glog.V(common.Log_Info_Level_1).Infof("%s is running. [rpc_addr = %s, node_id = %s]\n", cfg_node_id, server_addr, cfg_node_id)
	fmt.Printf("%s is running. [rpc_addr = %s, node_id = %s]\n", cfg_node_id, server_addr, cfg_node_id)

	// Handle SIGINT and SIGTERM.
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	glog.V(common.Log_Info_Level_1).Infoln("signal:", <-ch)

	// Stop the service gracefully.
	db_service.stop_grpc_service()
}
