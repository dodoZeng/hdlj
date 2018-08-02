//go:generate protoc -I ../proto --go_out=plugins=grpc:../proto/ ../proto/server.proto ../proto/client.proto ../proto/common.proto  ../proto/db.proto ../proto/db_roisoft_acct.proto ../proto/db_hdlj.proto

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

import (
	"github.com/golang/glog"
	"roisoft.com/hdlj/common"
)

func init() {
	//fmt.Println("main")
	bStopServer = true

	flag.String("cfg", cfg_file, "configuration file")
	flag.String("node_id", cfg_node_id, "node id")
	flag.String("node_name", cfg_node_name, "node name")
	flag.String("node_addr", cfg_node_addr, "ip")
	flag.String("node_port", cfg_node_port, "port")

	//runtime.GOMAXPROCS(runtime.NumCPU())
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

	cfg_table_path = common.GetAbsFilePath(cfg_table_path)

	return true, ""
}

/*
func start_grpc_service(s *grpc.Server, addr string) (bool, string) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		glog.Errorf("failed to listen: %v\n", err)
		return false, err.Error()
	}

	pb.RegisterUtilityServer(s, &grpc_service{})
	reflection.Register(s)
	go func() {
		defer glog.Infoln("routine exit: grpc.")
		defer fmt.Println("routine exit: grpc.")

		if err := s.Serve(lis); err != nil {
			glog.Errorf("failed to grpc serve: %v\n", err)
		}
	}()
	return true, ""
}
*/

func main() {
	// get config from file
	if ok, err := load_config(); !ok {
		fmt.Println(err)
		return
	}
	// get config from consul
	if ok, err := get_config_from_consul(); !ok {
		fmt.Printf("fail to get config from consul. [err = %s]\n", err)
		panic(1)
		return
	}

	os.MkdirAll(cfg_log_dir, 0777)
	fmt.Println("for detail, log in file (", cfg_log_dir, ")")

	defer glog.Flush()
	defer glog.V(common.Log_Info_Level_1).Infof("stop server. [node_id = %s]\n", cfg_node_id)
	glog.V(common.Log_Info_Level_1).Infof("start server. [node_id = %s]\n", cfg_node_id)

	// Make a new service and send it into the background.
	//grpc_service := grpc.NewServer()
	//if ok, err := start_grpc_service(grpc_service, grpc_service_ipaddr); !ok {
	//	glog.Errorln(err)
	//	return
	//}
	tpc_service := NewTcpService()
	if ok, err := tpc_service.Serve(fmt.Sprintf("%s:%s", cfg_node_addr, cfg_node_port)); !ok {
		glog.Errorln(err)
		panic(1)
		return
	}

	// 开启consul健康检查服务
	go startConsulCheckService()

	// 注册服务
	//if ok, err := registerServer(); !ok {
	//	glog.Errorln(err)
	//	return
	//}

	addr := fmt.Sprintf("%s:%s", cfg_node_addr, cfg_node_port)
	glog.V(common.Log_Info_Level_1).Infof("%s is running. [tcp_addr = %s, node_id = %s]\n", cfg_node_id, addr, cfg_node_id)
	fmt.Printf("%s is running. [tcp_addr = %s, node_id = %s]\n", cfg_node_id, addr, cfg_node_id)

	// Handle SIGINT and SIGTERM.
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	glog.V(common.Log_Info_Level_1).Infoln("signal:", <-ch)

	// Stop the service gracefully.
	tpc_service.Stop()
	//grpc_service.Stop()
}
