//go:generate protoc -I ../proto --go_out=plugins=grpc:../proto/ ../proto/client.proto ../proto/common.proto  ../proto/server.proto  ../proto/db.proto ../proto/db_roisoft_acct.proto ../proto/db_hdlj.proto

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	//"time"
)
import (
	"github.com/golang/glog"
	//"github.com/golang/protobuf/proto"
	"roisoft.com/hdlj/common"
	pb "roisoft.com/hdlj/proto"
)

func init() {
	flag.String("cfg", cfg_file, "configuration file")
	flag.String("node_id", cfg_node_id, "node id")
	flag.String("node_name", cfg_node_name, "node name")
	flag.String("node_addr", cfg_node_addr, "ip")
	flag.String("node_port", cfg_node_port, "port")
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

	// 解析node_id后缀
	pos := strings.LastIndex(cfg_node_id, ".")
	if pos == -1 {
		pos = 0
	} else {
		pos++
	}
	cfg_node_id_suffix = cfg_node_id[pos:]

	cfg_player_id_config = common.GetAbsFilePath(cfg_player_id_config)

	return true, ""
}

func test() {
	if client, ok := <-chRedisClients; ok {
		var data pb.ErrorInfo

		data.Code = 2
		data.Context = "dodo"

		//buf1, _ := proto.Marshal(&data)
		//client.SetNX("sessions.my_session_10", buf1, time.Duration(0))
		//buf2, _ := client.Get("sessions.my_session_10").Bytes()
		///if err := proto.Unmarshal(buf2, &data); err == nil {
		//	fmt.Println(time.Now().UnixNano(), "pass data.Code = ", data.Code)
		//}

		fmt.Println(client.Exists("sessions.my_session_10").Result())
	}
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

	//test()
	//return

	// Make a new service and send it into the background.
	server_addr := fmt.Sprintf("%s:%s", cfg_node_addr, cfg_node_port)
	grpc_service := NewGrpcService()
	if ok, err := grpc_service.start_grpc_service(server_addr); !ok {
		glog.Errorln(err)
		panic(1)
		return
	}

	// 启动consul检查服务
	//go startConsulCheckService()

	glog.V(common.Log_Info_Level_1).Infof("%s is running. [rpc_addr = %s, node_id = %s]\n", cfg_node_id, server_addr, cfg_node_id)
	fmt.Printf("%s is running. [rpc_addr = %s, node_id = %s]\n", cfg_node_id, server_addr, cfg_node_id)

	// Handle SIGINT and SIGTERM.
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	glog.V(common.Log_Info_Level_1).Infoln("signal:", <-ch)

	// Stop the service gracefully.
	grpc_service.stop_grpc_service()
	if ok, err := store_cur_player_id(); !ok {
		glog.Errorf("fail to store player id. [err = %s]\n", err)
	}
}
